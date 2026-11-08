package main

import (
	"strings"
	"testing"
)

func TestUpsertPartialKeyIsRefusedAndWritesNothing(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	before := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs")

	got := upsert(t, path, "--key", "sample=S1", "note=changed")

	requireFailure(t, got, 2, "key_mismatch")
	if !strings.Contains(got.Stderr, "replicate is missing") {
		t.Errorf("stderr = %q, want it to name the missing column", got.Stderr)
	}
	if after := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs"); after != before {
		t.Errorf("state changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestUpsertPartialKeyWritesNoRunOnAFreshDatabase(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	requireFailure(t, upsert(t, path, "--key", "sample=S1", "note=x"), 2, "key_mismatch")

	if got := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name IN ('tool_runs', 'rnaseq_qc_history')"); got != "0" {
		t.Errorf("tables created by a refused key = %s, want 0", got)
	}
}

func TestPartialKeyDoesNotSplitTheHistory(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireFailure(t, upsert(t, path, "--key", "sample=S1", "note=changed"), 2, "key_mismatch")

	if got := scalar(t, path, "SELECT count(DISTINCT record_key) FROM rnaseq_qc_history"); got != "1" {
		t.Errorf("record keys in the history = %s, want 1", got)
	}
	if got := scalar(t, path, "SELECT note FROM rnaseq_qc"); got != "ok" {
		t.Errorf("note = %s, want the value of the full-key write", got)
	}
	requireSuccess(t, upsertS1(t, path, "note=again"))
	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE source = 'reconciled'"); got != "0" {
		t.Errorf("reconciled rows = %s, want 0", got)
	}
}

func TestLogPartialKeyIsRefused(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	got := runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1")

	requireFailure(t, got, 2, "key_mismatch")
}

func TestKeyOnATableWithoutUniqueKeyIsRefused(t *testing.T) {
	t.Parallel()
	path := newDB(t, `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, note TEXT)`)

	requireFailure(t, upsertS1(t, path, "note=a"), 2, "no_unique_key")
	requireFailure(t, runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1"), 2, "no_unique_key")

	if got := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name IN ('tool_runs', 'rnaseq_qc_history')"); got != "0" {
		t.Errorf("tables created by a refused key = %s, want 0", got)
	}
}

func TestRefusedNumbersExplainTheFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		value string
		want  string
	}{
		{"percent into real", "Alignment_Rate", "94.3%", "→ drop the % sign: 94.3\n"},
		{"percent into integer", "Mapped_Reads", "12%", "→ drop the % sign: 12\n"},
		{"decimal percent into integer", "Mapped_Reads", "94.3%", "→ drop the % sign\n"},
		{"thousands by commas", "Alignment_Rate", "12,000,000", "→ drop the thousands separators: 12000000\n"},
		{"thousands by spaces", "Alignment_Rate", "12 000 000", "→ drop the thousands separators: 12000000\n"},
		{"ambiguous", "Alignment_Rate", "1,234", "→ use a dot as the decimal separator (1.234) or drop the thousands separator (1234)\n"},
		{"comma as decimal point", "Alignment_Rate", "94,3", "→ use a dot as the decimal separator: 94.3\n"},
		{"negative comma as decimal point", "Alignment_Rate", "-0,5", "→ use a dot as the decimal separator: -0.5\n"},
		{"word has no hint", "Alignment_Rate", "abc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := newDB(t, qcTable)

			got := upsertS1(t, path, tt.field+"="+tt.value)

			requireFailure(t, got, 3, "invalid_value")
			lines := strings.SplitN(got.Stderr, "\n", 2)
			if lines[1] != tt.want {
				t.Errorf("context lines = %q, want %q", lines[1], tt.want)
			}
		})
	}
}

func TestMissingTableSuggestsANearbyOne(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	got := runScribo(t, "upsert", "--db", path, "--table", "rnaseqqc", "--key", "sample=S1", "--key", "replicate=1", "note=x")

	requireFailure(t, got, 2, "table_not_found")
	if !strings.Contains(got.Stderr, `→ did you mean "rnaseq_qc"?`) {
		t.Errorf("stderr = %q, want the suggestion", got.Stderr)
	}
	if strings.Contains(got.Stderr, "schema") {
		t.Errorf("stderr = %q, a near match needs no hint about creating a table", got.Stderr)
	}
}

func TestMissingTableListsTheDataTablesOnly(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable, `CREATE TABLE samples_extra (a TEXT, UNIQUE(a))`, `CREATE TABLE samples (id TEXT)`)

	got := runScribo(t, "log", "--db", path, "--table", "zzzzzzzzzz", "--key", "a=1")

	requireFailure(t, got, 2, "table_not_found")
	if !strings.Contains(got.Stderr, "→ tables: rnaseq_qc, samples_extra; scribo schema prints the CREATE TABLE of a new one\n") {
		t.Errorf("stderr = %q, want the data tables without the reserved samples", got.Stderr)
	}
}

func TestMissingTableOnAnEmptyDatabase(t *testing.T) {
	t.Parallel()
	path := newDB(t, `CREATE TABLE gone (a TEXT)`, `DROP TABLE gone`)

	got := runScribo(t, "log", "--db", path, "--table", "x", "--key", "a=1")

	requireFailure(t, got, 2, "table_not_found")
	if !strings.Contains(got.Stderr, "→ the database has no data table yet; scribo schema prints the CREATE TABLE of a new one\n") {
		t.Errorf("stderr = %q", got.Stderr)
	}
}

func TestUpsertWithoutKeyExplainsWhereItGoes(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	got := upsert(t, path, "sample=S1", "Alignment_Rate=94.3")

	requireFailure(t, got, 1, "usage")
	want := "ERROR [usage]: --key is required\n→ the record key goes in --key, for example --key sample=S1\n"
	if got.Stderr != want {
		t.Errorf("stderr = %q, want %q", got.Stderr, want)
	}
}

func TestUpsertWithoutKeyOrArgumentsIsPlainUsage(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	got := upsert(t, path)

	requireFailure(t, got, 1, "usage")
	if !strings.HasPrefix(got.Stderr, "ERROR [usage]: --key is required\n→ run 'scribo upsert --help'") {
		t.Errorf("stderr = %q", got.Stderr)
	}
}

func TestMissingTableSuggestsByPrefix(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	got := runScribo(t, "upsert", "--db", path, "--table", "rnaseq", "--key", "sample=S1", "--key", "replicate=1", "note=x")

	requireFailure(t, got, 2, "table_not_found")
	if !strings.Contains(got.Stderr, `→ did you mean "rnaseq_qc"?`) {
		t.Errorf("stderr = %q, want the suggestion by prefix", got.Stderr)
	}
}

func TestKeyMismatchListsSeveralColumnsInPlural(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	got := upsert(t, path, "--key", "note=x", "--key", "run_date=y", "Mapped_Reads=1")

	requireFailure(t, got, 2, "key_mismatch")
	want := "the key of rnaseq_qc is (replicate, sample); replicate, sample are missing; note, run_date are not part of it"
	if !strings.Contains(got.Stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", got.Stderr, want)
	}
}
