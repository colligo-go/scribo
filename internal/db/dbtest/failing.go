package dbtest

import (
	"context"
	"errors"
	"time"

	"github.com/colligo-go/scribo/internal/db"
)

var ErrInjected = errors.New("injected failure")

type Failing struct {
	db.Backend
	FailAt    int
	FailNamed string
	Calls     int
	Failed    string
}

func (f *Failing) fail(name string) error {
	f.Calls++
	if f.Calls == f.FailAt {
		f.Failed = name
		return ErrInjected
	}
	if name == f.FailNamed {
		return ErrInjected
	}
	return nil
}

func (f *Failing) Reached() bool {
	return f.Calls >= f.FailAt
}

func (f *Failing) Open(ctx context.Context, path string, opts db.OpenOptions) error {
	if err := f.fail("Open"); err != nil {
		return err
	}
	return f.Backend.Open(ctx, path, opts)
}

func (f *Failing) TableExists(ctx context.Context, table string) (bool, error) {
	if err := f.fail("TableExists"); err != nil {
		return false, err
	}
	return f.Backend.TableExists(ctx, table)
}

func (f *Failing) Columns(ctx context.Context, table string) ([]db.Column, error) {
	if err := f.fail("Columns"); err != nil {
		return nil, err
	}
	return f.Backend.Columns(ctx, table)
}

func (f *Failing) UniqueKeys(ctx context.Context, table string) ([][]string, error) {
	if err := f.fail("UniqueKeys"); err != nil {
		return nil, err
	}
	return f.Backend.UniqueKeys(ctx, table)
}

func (f *Failing) EnsureContractTables(ctx context.Context) error {
	if err := f.fail("EnsureContractTables"); err != nil {
		return err
	}
	return f.Backend.EnsureContractTables(ctx)
}

func (f *Failing) EnsureHistoryTable(ctx context.Context, table string) error {
	if err := f.fail("EnsureHistoryTable"); err != nil {
		return err
	}
	return f.Backend.EnsureHistoryTable(ctx, table)
}

func (f *Failing) SelectRows(ctx context.Context, table string, where []db.Predicate, fields []string, limit int) (db.Rows, error) {
	if err := f.fail("SelectRows"); err != nil {
		return db.Rows{}, err
	}
	return f.Backend.SelectRows(ctx, table, where, fields, limit)
}

func (f *Failing) HistoryOf(ctx context.Context, table, recordKey, field string) ([]db.HistoryRow, error) {
	if err := f.fail("HistoryOf"); err != nil {
		return nil, err
	}
	return f.Backend.HistoryOf(ctx, table, recordKey, field)
}

func (f *Failing) RecordExists(ctx context.Context, table string, key db.Key) (bool, error) {
	if err := f.fail("RecordExists"); err != nil {
		return false, err
	}
	return f.Backend.RecordExists(ctx, table, key)
}

func (f *Failing) Runs(ctx context.Context, ids []int64) (map[int64]db.RunInfo, error) {
	if err := f.fail("Runs"); err != nil {
		return nil, err
	}
	return f.Backend.Runs(ctx, ids)
}

func (f *Failing) LastRowOfRun(ctx context.Context, table string, runID int64) (db.HistoryRow, bool, error) {
	if err := f.fail("LastRowOfRun"); err != nil {
		return db.HistoryRow{}, false, err
	}
	return f.Backend.LastRowOfRun(ctx, table, runID)
}

func (f *Failing) BeginRun(ctx context.Context, meta db.RunMeta) (int64, error) {
	if err := f.fail("BeginRun"); err != nil {
		return 0, err
	}
	return f.Backend.BeginRun(ctx, meta)
}

func (f *Failing) EndRun(ctx context.Context, runID int64, exitCode int) error {
	if err := f.fail("EndRun"); err != nil {
		return err
	}
	return f.Backend.EndRun(ctx, runID, exitCode)
}

func (f *Failing) BeginTx(ctx context.Context) (db.Tx, error) {
	if err := f.fail("BeginTx"); err != nil {
		return nil, err
	}
	tx, err := f.Backend.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{Tx: tx, backend: f}, nil
}

type failingTx struct {
	db.Tx
	backend *Failing
}

func (t *failingTx) SelectRow(ctx context.Context, table string, key db.Key, fields []string) (db.Row, bool, error) {
	if err := t.backend.fail("SelectRow"); err != nil {
		return nil, false, err
	}
	return t.Tx.SelectRow(ctx, table, key, fields)
}

func (t *failingTx) ProjectedRow(ctx context.Context, table, recordKey string) (db.Row, error) {
	if err := t.backend.fail("ProjectedRow"); err != nil {
		return nil, err
	}
	return t.Tx.ProjectedRow(ctx, table, recordKey)
}

func (t *failingTx) Upsert(ctx context.Context, table string, key db.Key, values db.Values, mode db.WriteMode) ([]db.FieldChange, error) {
	if err := t.backend.fail("Upsert"); err != nil {
		return nil, err
	}
	return t.Tx.Upsert(ctx, table, key, values, mode)
}

func (t *failingTx) AppendHistory(ctx context.Context, table, recordKey string, changes []db.FieldChange, runID int64, at time.Time, source db.HistorySource) error {
	if err := t.backend.fail("AppendHistory"); err != nil {
		return err
	}
	return t.Tx.AppendHistory(ctx, table, recordKey, changes, runID, at, source)
}

func (t *failingTx) Commit() error {
	if err := t.backend.fail("Commit"); err != nil {
		return err
	}
	return t.Tx.Commit()
}

func (t *failingTx) Rollback() error {
	err := t.Tx.Rollback()
	if failure := t.backend.fail("Rollback"); failure != nil {
		return failure
	}
	return err
}
