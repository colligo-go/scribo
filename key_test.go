package main

import (
	"strings"
	"testing"
)

func TestUpsertPartialKeyIsRefusedAndWritesNothing(t *testing.T) {
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
	path := newDB(t, qcTable)

	requireFailure(t, upsert(t, path, "--key", "sample=S1", "note=x"), 2, "key_mismatch")

	if got := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name IN ('tool_runs', 'rnaseq_qc_history')"); got != "0" {
		t.Errorf("tables created by a refused key = %s, want 0", got)
	}
}

func TestPartialKeyDoesNotSplitTheHistory(t *testing.T) {
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
	path := seeded(t)

	got := runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1")

	requireFailure(t, got, 2, "key_mismatch")
}

func TestKeyOnATableWithoutUniqueKeyIsRefused(t *testing.T) {
	path := newDB(t, `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, note TEXT)`)

	requireFailure(t, upsertS1(t, path, "note=a"), 2, "no_unique_key")
	requireFailure(t, runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1"), 2, "no_unique_key")

	if got := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name IN ('tool_runs', 'rnaseq_qc_history')"); got != "0" {
		t.Errorf("tables created by a refused key = %s, want 0", got)
	}
}
