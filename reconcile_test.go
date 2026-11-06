package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func mustExec(t *testing.T, path, statement string) {
	t.Helper()
	if _, err := execRaw(path, statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func historyOf(t *testing.T, path, field string) [][]string {
	t.Helper()
	return query(t, path, `SELECT source, ifnull(old_value, 'NULL'), old_is_null, ifnull(new_value, 'NULL'), new_is_null, changed_at, id
		FROM rnaseq_qc_history WHERE field = ? ORDER BY changed_at, id`, field)
}

func requireReconcileWarning(t *testing.T, got result, mentions ...string) {
	t.Helper()
	if got.ExitCode != 0 || got.Stdout != "" {
		t.Fatalf("exit %d, stdout %q", got.ExitCode, got.Stdout)
	}
	if !strings.Contains(got.Stderr, "WARNING [history_reconciled]") {
		t.Fatalf("stderr = %q, want a history_reconciled warning", got.Stderr)
	}
	for _, mention := range mentions {
		if !strings.Contains(got.Stderr, mention) {
			t.Errorf("stderr = %q, want it to mention %q", got.Stderr, mention)
		}
	}
}

func requireChain(t *testing.T, path string) {
	t.Helper()
	for _, field := range []string{"Alignment_Rate", "Mapped_Reads", "note", "run_date"} {
		rows := historyOf(t, path, field)
		for i, row := range rows {
			if i == 0 {
				if row[2] != "1" {
					t.Errorf("%s: first row %v has a non-NULL old value", field, row)
				}
				continue
			}
			previous := rows[i-1]
			if row[1] != previous[3] || row[2] != previous[4] {
				t.Errorf("%s: row %v does not continue %v", field, row, previous)
			}
		}
	}
}

func TestReconcileOutsideEdit(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'edited'")

	got := upsertS1(t, path, "Mapped_Reads=13")

	requireReconcileWarning(t, got, "note", `[["replicate","1"],["sample","S1"]]`)
	for _, value := range []string{"edited", "ok"} {
		if strings.Contains(got.Stderr, value) {
			t.Errorf("warning leaks the value %q: %s", value, got.Stderr)
		}
	}
	notes := historyOf(t, path, "note")
	if len(notes) != 2 || fmt.Sprint(notes[1][:5]) != "[reconciled ok 0 edited 0]" {
		t.Errorf("note history = %v", notes)
	}
	reads := historyOf(t, path, "Mapped_Reads")
	if len(reads) != 2 || fmt.Sprint(reads[1][:5]) != "[scribo 12 0 13 0]" {
		t.Errorf("Mapped_Reads history = %v", reads)
	}
}

func TestReconcileComesBeforeTheWriteOnTheSameField(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET Mapped_Reads = 50")

	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=13"), "Mapped_Reads")

	rows := historyOf(t, path, "Mapped_Reads")
	if len(rows) != 3 {
		t.Fatalf("Mapped_Reads history = %v", rows)
	}
	reconciled, written := rows[1], rows[2]
	if fmt.Sprint(reconciled[:5]) != "[reconciled 12 0 50 0]" || fmt.Sprint(written[:5]) != "[scribo 50 0 13 0]" {
		t.Errorf("rows = %v", rows)
	}
	if reconciled[5] != written[5] {
		t.Errorf("changed_at differs: %s vs %s", reconciled[5], written[5])
	}
	reconciledID, _ := strconv.Atoi(reconciled[6])
	writtenID, _ := strconv.Atoi(written[6])
	if reconciledID >= writtenID {
		t.Errorf("reconciled id %s is not below scribo id %s", reconciled[6], written[6])
	}
}

func TestReconcileIgnoresAnEditThatWasUndone(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'other'")
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'ok'")

	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=13"))

	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE source = 'reconciled'"); got != "0" {
		t.Errorf("reconciled rows = %s", got)
	}
}

func TestReconcileOutsideNull(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = NULL")

	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=13"), "note")

	notes := historyOf(t, path, "note")
	if len(notes) != 2 || fmt.Sprint(notes[1][:5]) != "[reconciled ok 0 NULL 1]" {
		t.Errorf("note history = %v", notes)
	}
}

func TestReconcileIgnoresFieldsWithoutHistory(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET run_date = '2026-01-01 00:00:00'")

	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=13"))

	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE field = 'run_date'"); got != "0" {
		t.Errorf("run_date history rows = %s", got)
	}
}

func TestReconcileDeletedRecord(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "DELETE FROM rnaseq_qc")

	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=20"), "record deleted outside Scribo")

	reconciled := query(t, path, `SELECT field, ifnull(new_value, 'NULL'), new_is_null FROM rnaseq_qc_history WHERE source = 'reconciled' ORDER BY field`)
	if fmt.Sprint(reconciled) != "[[Alignment_Rate NULL 1] [Mapped_Reads NULL 1] [note NULL 1]]" {
		t.Errorf("reconciled rows = %v", reconciled)
	}
	reads := historyOf(t, path, "Mapped_Reads")
	if len(reads) != 3 || fmt.Sprint(reads[2][:5]) != "[scribo NULL 1 20 0]" {
		t.Errorf("Mapped_Reads history = %v", reads)
	}
}

func TestReconcileComparesTheStoredForm(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET Alignment_Rate = '94.30'")

	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=13"))

	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE source = 'reconciled'"); got != "0" {
		t.Errorf("reconciled rows = %s", got)
	}
}

func TestReconcileRowsRollBackWithAFailedWrite(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'x'")

	got := upsertS1(t, path, "--insert-only", "Mapped_Reads=13")

	requireFailure(t, got, 3, "key_already_exists")
	if strings.Contains(got.Stderr, "history_reconciled") {
		t.Errorf("warned about a reconciliation that was rolled back: %s", got.Stderr)
	}
	if n := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE source = 'reconciled'"); n != "0" {
		t.Errorf("reconciled rows = %s, want 0", n)
	}
	requireOneFailedRun(t, path, 1)
}

func TestHistoryChainStaysContinuous(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'edited'")
	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=13"))
	mustExec(t, path, "UPDATE rnaseq_qc SET Mapped_Reads = 50")
	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=13"))
	mustExec(t, path, "DELETE FROM rnaseq_qc")
	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=20", "note=back"))

	requireChain(t, path)
}

func TestReconcileSkipsAColumnDroppedFromTheTable(t *testing.T) {
	path := seeded(t)
	before := historyOf(t, path, "note")
	mustExec(t, path, "ALTER TABLE rnaseq_qc DROP COLUMN note")

	got := upsertS1(t, path, "Mapped_Reads=13")

	if got.ExitCode != 0 || got.Stdout != "" || got.Stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q: a dropped column must not block the record", got.ExitCode, got.Stdout, got.Stderr)
	}
	if after := historyOf(t, path, "note"); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("note history changed after the drop: before %v, after %v", before, after)
	}
	reads := historyOf(t, path, "Mapped_Reads")
	if len(reads) != 2 || fmt.Sprint(reads[1][:5]) != "[scribo 12 0 13 0]" {
		t.Errorf("Mapped_Reads history = %v", reads)
	}
}
