package writer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/dbtest"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
)

const qcTable = `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, note TEXT, UNIQUE(sample, replicate))`

func ignore(exitcode.Warning) {}

func execute(t *testing.T, path string, statements ...string) {
	t.Helper()
	handle, err := sql.Open("sqlite", "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	for _, statement := range statements {
		if _, err := handle.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func dump(t *testing.T, path, statement string) string {
	t.Helper()
	handle, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	rows, err := handle.Query(statement)
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&out, cells...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func state(t *testing.T, path string) string {
	return dump(t, path, "SELECT * FROM rnaseq_qc ORDER BY sample, replicate") +
		dump(t, path, `SELECT record_key, field, old_value, old_is_null, new_value, new_is_null, source
			FROM rnaseq_qc_history ORDER BY record_key, field, changed_at, id`)
}

func baseDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "base.db")
	execute(t, path, qcTable)
	seed := Batch{DB: path, Table: "rnaseq_qc", Records: []Record{
		{Key: db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}}, Values: db.Values{"note": {Raw: "a"}}, Name: "S1"},
		{Key: db.Key{"sample": {Raw: "S2"}, "replicate": {Raw: "1"}}, Values: db.Values{"note": {Raw: "a"}}, Name: "S2"},
	}}
	if err := UpsertMany(context.Background(), sqlite.New(), seed, ignore); err != nil {
		t.Fatal(err)
	}
	execute(t, path, "UPDATE rnaseq_qc SET note = 'edited' WHERE sample = 'S1'")
	return path
}

func copyOf(t *testing.T, source string) string {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "study.db")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sweep(t *testing.T, write func(backend db.Backend, path string, warn func(exitcode.Warning)) error) {
	t.Helper()
	base := baseDatabase(t)
	before := state(t, base)
	whole := copyOf(t, base)
	if err := write(sqlite.New(), whole, ignore); err != nil {
		t.Fatal(err)
	}
	after := state(t, whole)
	if after == before {
		t.Fatal("the write changes nothing, so the sweep proves nothing")
	}

	for failAt := 1; ; failAt++ {
		path := copyOf(t, base)
		backend := &dbtest.Failing{Backend: sqlite.New(), FailAt: failAt}
		var warnings []exitcode.Warning

		err := write(backend, path, func(w exitcode.Warning) { warnings = append(warnings, w) })

		if !backend.Reached() {
			if err != nil {
				t.Fatalf("no failure injected, yet: %v", err)
			}
			if failAt < 5 {
				t.Fatalf("the write made only %d backend calls", failAt-1)
			}
			return
		}
		got := state(t, path)
		switch {
		case err == nil && got != after:
			t.Errorf("call %d (%s) failed, the write reported success, and the state is not the whole write:\n%s", failAt, backend.Failed, got)
		case err == nil && backend.Failed == "EndRun" && !closeWarned(warnings):
			t.Errorf("call %d: the closing EndRun failed without a warning", failAt)
		case err == nil && backend.Failed != "EndRun" && backend.Failed != "Rollback":
			t.Errorf("call %d (%s) failed and the write reported success", failAt, backend.Failed)
		case err != nil && !errors.Is(err, dbtest.ErrInjected):
			t.Errorf("call %d (%s) failed: error %v is not the injected failure", failAt, backend.Failed, err)
		case err != nil && got != before:
			t.Errorf("call %d (%s) failed and the state changed:\nbefore %s\nafter  %s", failAt, backend.Failed, before, got)
		}
		runs := dump(t, path, "SELECT ifnull(exit_code, 'NULL') FROM tool_runs ORDER BY id")
		if err != nil && strings.HasSuffix(runs, "0\n") && strings.Count(runs, "\n") > 1 {
			t.Errorf("call %d failed, yet the last run closed with exit 0: %q", failAt, runs)
		}
	}
}

func closeWarned(warnings []exitcode.Warning) bool {
	for _, warning := range warnings {
		if warning.Code == exitcode.CodeInternal && strings.Contains(warning.Message, "the write was committed") {
			return true
		}
	}
	return false
}

func TestEveryBackendFailureOfAnUpsertWritesNothing(t *testing.T) {
	sweep(t, func(backend db.Backend, path string, warn func(exitcode.Warning)) error {
		return Upsert(context.Background(), backend, Request{
			DB: path, Table: "rnaseq_qc",
			Key:    db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}},
			Values: db.Values{"note": {Raw: "b"}},
		}, warn)
	})
}

func TestEveryBackendFailureOfABatchWritesNothing(t *testing.T) {
	sweep(t, func(backend db.Backend, path string, warn func(exitcode.Warning)) error {
		return UpsertMany(context.Background(), backend, Batch{DB: path, Table: "rnaseq_qc", Records: []Record{
			{Key: db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}}, Values: db.Values{"note": {Raw: "b"}}, Name: "S1"},
			{Key: db.Key{"sample": {Raw: "S2"}, "replicate": {Raw: "1"}}, Values: db.Values{"note": {Raw: "b"}}, Name: "S2"},
			{Key: db.Key{"sample": {Raw: "S3"}, "replicate": {Raw: "1"}}, Values: db.Values{"note": {Raw: "b"}}, Name: "S3"},
		}}, warn)
	})
}

func TestEveryBackendFailureOfADryRunIsReturned(t *testing.T) {
	base := baseDatabase(t)
	before := state(t, base)
	for failAt := 1; ; failAt++ {
		path := copyOf(t, base)
		backend := &dbtest.Failing{Backend: sqlite.New(), FailAt: failAt}

		changes, err := DryRun(context.Background(), backend, Request{
			DB: path, Table: "rnaseq_qc",
			Key:    db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}},
			Values: db.Values{"note": {Raw: "b"}},
		}, ignore)

		if got := state(t, path); got != before {
			t.Fatalf("call %d: a dry run changed the state", failAt)
		}
		if !backend.Reached() {
			if err != nil || len(changes) != 2 {
				t.Fatalf("no failure injected: changes %+v, error %v, want the reconciled and the written change", changes, err)
			}
			return
		}
		if backend.Failed == "Rollback" && err == nil && len(changes) == 2 {
			continue
		}
		if !errors.Is(err, dbtest.ErrInjected) {
			t.Errorf("call %d (%s) failed: error %v is not the injected failure", failAt, backend.Failed, err)
		}
	}
}

func TestAFailedWriteWhoseRunCannotBeClosedKeepsItsOwnError(t *testing.T) {
	base := baseDatabase(t)
	before := state(t, base)
	runsBefore := dump(t, base, "SELECT count(*) FROM tool_runs")
	for failAt := 1; ; failAt++ {
		path := copyOf(t, base)
		backend := &dbtest.Failing{Backend: sqlite.New(), FailAt: failAt, FailNamed: "EndRun"}
		var warnings []string

		err := Upsert(context.Background(), backend, Request{
			DB: path, Table: "rnaseq_qc",
			Key:    db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}},
			Values: db.Values{"note": {Raw: "b"}},
		}, func(w exitcode.Warning) { warnings = append(warnings, w.Message) })

		if !backend.Reached() {
			if err != nil || !strings.Contains(strings.Join(warnings, "|"), "the write was committed") {
				t.Fatalf("no failure injected but EndRun: error %v, warnings %q", err, warnings)
			}
			return
		}
		began := dump(t, path, "SELECT count(*) FROM tool_runs") != runsBefore
		if err == nil || !began {
			continue
		}
		if !errors.Is(err, dbtest.ErrInjected) || state(t, path) != before {
			t.Errorf("call %d (%s) failed: error %v, or the state changed", failAt, backend.Failed, err)
		}
		if want := "could not close run"; !strings.Contains(strings.Join(warnings, "|"), want) {
			t.Errorf("call %d (%s) failed and EndRun failed too: warnings %q, want %q", failAt, backend.Failed, warnings, want)
		}
	}
}
