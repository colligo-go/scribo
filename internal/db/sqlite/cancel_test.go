package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/db"
)

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func requireCancelled(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("%s with a cancelled context: err = %v, want context.Canceled", name, err)
	}
}

func TestEveryBackendMethodReturnsTheCancellation(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()
	if err := backend.EnsureHistoryTable(ctx, "rnaseq_qc"); err != nil {
		t.Fatal(err)
	}
	runID, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	gone := cancelled()
	where := []db.Predicate{{Field: "sample", Op: db.OpEq, Value: db.Value{Raw: "S1"}}}

	calls := map[string]func() error{
		"TableExists":          func() error { _, err := backend.TableExists(gone, "rnaseq_qc"); return err },
		"Tables":               func() error { _, err := backend.Tables(gone); return err },
		"Columns":              func() error { _, err := backend.Columns(gone, "rnaseq_qc"); return err },
		"EnsureContractTables": func() error { return backend.EnsureContractTables(gone) },
		"EnsureHistoryTable":   func() error { return backend.EnsureHistoryTable(gone, "rnaseq_qc") },
		"UniqueKeys":           func() error { _, err := backend.UniqueKeys(gone, "rnaseq_qc"); return err },
		"SelectRows":           func() error { _, err := backend.SelectRows(gone, "rnaseq_qc", where, nil, 0); return err },
		"BeginTx":              func() error { _, err := backend.BeginTx(gone); return err },
		"HistoryOf": func() error {
			_, err := backend.HistoryOf(gone, "rnaseq_qc", `[["replicate","1"],["sample","S1"]]`, "")
			return err
		},
		"RecordExists": func() error { _, err := backend.RecordExists(gone, "rnaseq_qc", s1()); return err },
		"Runs":         func() error { _, err := backend.Runs(gone, []int64{runID}); return err },
		"LastRowOfRun": func() error { _, _, err := backend.LastRowOfRun(gone, "rnaseq_qc", runID); return err },
		"BeginRun":     func() error { _, err := backend.BeginRun(gone, db.RunMeta{ToolVersion: "test"}); return err },
		"EndRun":       func() error { return backend.EndRun(gone, runID, 0) },
	}
	for name, call := range calls {
		requireCancelled(t, name, call())
	}
}

func TestEveryTxMethodReturnsTheCancellation(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()
	if err := backend.EnsureHistoryTable(ctx, "rnaseq_qc"); err != nil {
		t.Fatal(err)
	}
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gone := cancelled()
	recordKey := `[["replicate","1"],["sample","S1"]]`
	change := []db.FieldChange{{Field: "note", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: "ok"}}}

	calls := map[string]func() error{
		"SelectRow":    func() error { _, _, err := tx.SelectRow(gone, "rnaseq_qc", s1(), []string{"note"}); return err },
		"ProjectedRow": func() error { _, err := tx.ProjectedRow(gone, "rnaseq_qc", recordKey); return err },
		"Upsert": func() error {
			_, err := tx.Upsert(gone, "rnaseq_qc", s1(), db.Values{"note": {Raw: "ok"}}, db.ModeUpsert)
			return err
		},
		"AppendHistory": func() error {
			return tx.AppendHistory(gone, "rnaseq_qc", recordKey, change, 1, time.Now(), db.SourceScribo)
		},
	}
	for name, call := range calls {
		requireCancelled(t, name, call())
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := backend.db.QueryRow("SELECT count(*) FROM rnaseq_qc").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rnaseq_qc has %d rows after cancelled calls (err %v), want 0", rows, err)
	}
}
