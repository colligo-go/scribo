package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const dryRunHeader = "source\tfield\told_value\told_is_null\tnew_value\tnew_is_null\n"

var everyTable = []string{"sqlite_schema", "rnaseq_qc", "rnaseq_qc_history", "tool_runs"}

func dryRun(t *testing.T, path string, args ...string) result {
	t.Helper()
	return upsert(t, path, append([]string{"--dry-run", "--key", "sample=S1", "--key", "replicate=1"}, args...)...)
}

func TestDryRunOfANewRecordWritesNothing(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	before := snapshot(t, path, everyTable...)

	got := upsert(t, path, "--dry-run", "--key", "sample=S2", "--key", "replicate=1", "Alignment_Rate=91", "note=new")

	want := dryRunHeader + "scribo\tAlignment_Rate\t\t1\t91\t0\nscribo\tnote\t\t1\tnew\t0\n"
	if got.ExitCode != 0 || got.Stdout != want || got.Stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q, want %q", got.ExitCode, got.Stdout, got.Stderr, want)
	}
	if after := snapshot(t, path, everyTable...); after != before {
		t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestDryRunCreatesNoTable(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)
	before := snapshot(t, path, "sqlite_schema", "rnaseq_qc")

	got := dryRun(t, path, "Mapped_Reads=12")

	if got.ExitCode != 0 || got.Stdout != dryRunHeader+"scribo\tMapped_Reads\t\t1\t12\t0\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", got.ExitCode, got.Stdout, got.Stderr)
	}
	if after := snapshot(t, path, "sqlite_schema", "rnaseq_qc"); after != before {
		t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestDryRunShowsTheReconciliationWithoutWarning(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'edited'")
	before := snapshot(t, path, everyTable...)

	got := dryRun(t, path, "note=new")

	want := dryRunHeader + "reconciled\tnote\tok\t0\tedited\t0\nscribo\tnote\tedited\t0\tnew\t0\n"
	if got.ExitCode != 0 || got.Stdout != want || got.Stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q, want %q and no warning", got.ExitCode, got.Stdout, got.Stderr, want)
	}
	if after := snapshot(t, path, everyTable...); after != before {
		t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestDryRunFailsLikeTheWrite(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireFailure(t, dryRun(t, path, "--insert-only", "note=x"), 3, "key_already_exists")
	requireFailure(t, dryRun(t, path, "Mapped_Reads=1.5"), 3, "invalid_value")
	requireFailure(t, upsert(t, path, "--dry-run", "--key", "sample=S1", "note=x"), 2, "key_mismatch")

	if got := count(t, path, "tool_runs"); got != "1" {
		t.Errorf("tool_runs rows = %s, want the seed's 1", got)
	}
}

func TestDryRunOfTheSameValueShowsOnlyTheHeader(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	got := dryRun(t, path, "Mapped_Reads=12", "Alignment_Rate=94.30")

	if got.ExitCode != 0 || got.Stdout != dryRunHeader {
		t.Errorf("exit %d, stdout %q, stderr %q", got.ExitCode, got.Stdout, got.Stderr)
	}
}

func TestDryRunPredictsTheWrite(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET Mapped_Reads = 99")
	args := []string{"Mapped_Reads=100", "--null", "note", "Alignment_Rate=1e2"}

	predicted := dryRun(t, path, args...)
	if predicted.ExitCode != 0 {
		t.Fatalf("dry run: exit %d, stderr %q", predicted.ExitCode, predicted.Stderr)
	}
	write := upsertS1(t, path, args...)
	if write.ExitCode != 0 {
		t.Fatalf("write: exit %d, stderr %q", write.ExitCode, write.Stderr)
	}

	rows := query(t, path, `SELECT source, field, ifnull(old_value, ''), old_is_null, ifnull(new_value, ''), new_is_null
		FROM rnaseq_qc_history WHERE run_id = (SELECT max(id) FROM tool_runs) ORDER BY id`)
	var recorded strings.Builder
	recorded.WriteString(dryRunHeader)
	for _, row := range rows {
		recorded.WriteString(strings.Join(row, "\t") + "\n")
	}
	if predicted.Stdout != recorded.String() {
		t.Errorf("dry run predicted:\n%s\nthe write recorded:\n%s", predicted.Stdout, recorded.String())
	}
	if len(rows) != 4 {
		t.Errorf("recorded %d rows, want 1 reconciled and 3 scribo", len(rows))
	}
}

func TestDryRunJSON(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	got := dryRun(t, path, "--format", "json", "--null", "note")

	var line map[string]any
	if err := json.Unmarshal([]byte(got.Stdout), &line); err != nil {
		t.Fatalf("stdout %q: %v", got.Stdout, err)
	}
	if line["source"] != "scribo" || line["field"] != "note" || line["old"] != "ok" || line["new"] != nil || len(line) != 4 {
		t.Errorf("line = %v", line)
	}
	requireFailure(t, upsertS1(t, path, "--format", "json", "note=x"), 1, "usage")
	requireFailure(t, dryRun(t, path, "--format", "xml", "note=x"), 1, "usage")
}
