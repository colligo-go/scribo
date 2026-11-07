package writer

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

type endRunCall struct {
	runID       int64
	exitCode    int
	ctxCanceled bool
}

type fakeBackend struct {
	db.Backend

	mu         sync.Mutex
	beginRuns  int
	endRuns    []endRunCall
	txStarted  chan struct{}
	rolledBack bool
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{txStarted: make(chan struct{})}
}

func (f *fakeBackend) Open(context.Context, string, db.OpenOptions) error { return nil }
func (f *fakeBackend) Close() error                                       { return nil }
func (f *fakeBackend) Warnings() []exitcode.Warning                       { return nil }

func (f *fakeBackend) TableExists(context.Context, string) (bool, error) { return true, nil }

func (f *fakeBackend) Columns(context.Context, string) ([]db.Column, error) {
	return []db.Column{
		{Name: "sample", DeclaredType: "TEXT", Affinity: "TEXT", Declared: true, Nullable: true},
		{Name: "replicate", DeclaredType: "INTEGER", Affinity: "INTEGER", Declared: true, Nullable: true},
		{Name: "note", DeclaredType: "TEXT", Affinity: "TEXT", Declared: true, Nullable: true},
	}, nil
}

func (f *fakeBackend) UniqueKeys(context.Context, string) ([][]string, error) {
	return [][]string{{"replicate", "sample"}}, nil
}

func (f *fakeBackend) EnsureContractTables(context.Context) error       { return nil }
func (f *fakeBackend) EnsureHistoryTable(context.Context, string) error { return nil }

func (f *fakeBackend) BeginRun(context.Context, db.RunMeta) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginRuns++
	return 7, nil
}

func (f *fakeBackend) EndRun(ctx context.Context, runID int64, exitCode int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endRuns = append(f.endRuns, endRunCall{runID, exitCode, ctx.Err() != nil})
	return nil
}

func (f *fakeBackend) BeginTx(ctx context.Context) (db.Tx, error) {
	close(f.txStarted)
	<-ctx.Done()
	return &fakeTx{backend: f}, nil
}

type fakeTx struct {
	db.Tx
	backend *fakeBackend
}

func (f *fakeTx) ProjectedRow(ctx context.Context, _ string, _ string) (db.Row, error) {
	return nil, ctx.Err()
}

func (f *fakeTx) Rollback() error {
	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()
	f.backend.rolledBack = true
	return nil
}

func request() Request {
	return Request{
		DB:     "unused.db",
		Table:  "rnaseq_qc",
		Key:    db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}},
		Values: db.Values{"note": {Raw: "x"}},
	}
}

func requireInterrupted(t *testing.T, err error) {
	t.Helper()
	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("error = %v, want an *exitcode.Error", err)
	}
	if domain.Status != exitcode.System || domain.Code != exitcode.CodeInterrupted {
		t.Errorf("status %d code %q, want %d %q", domain.Status, domain.Code, exitcode.System, exitcode.CodeInterrupted)
	}
	if domain.Context != "the transaction was rolled back; nothing was written" {
		t.Errorf("context = %q", domain.Context)
	}
}

func TestUpsertInterruptedInsideTheTransactionClosesTheRunWithALiveContext(t *testing.T) {
	backend := newFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Upsert(ctx, backend, request(), func(exitcode.Warning) {})
	}()

	<-backend.txStarted
	cancel()
	err := <-done

	requireInterrupted(t, err)
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.endRuns) != 1 {
		t.Fatalf("EndRun calls = %+v, want exactly one", backend.endRuns)
	}
	if call := backend.endRuns[0]; call.runID != 7 || call.exitCode != exitcode.System || call.ctxCanceled {
		t.Errorf("EndRun = %+v, want run 7, exit 5 and a context that is not cancelled", call)
	}
	if !backend.rolledBack {
		t.Error("the transaction was not rolled back")
	}
}

func TestUpsertInterruptedBeforeTheRunNeverBeginsIt(t *testing.T) {
	backend := newFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Upsert(ctx, backend, request(), func(exitcode.Warning) {})

	requireInterrupted(t, err)
	if backend.beginRuns != 0 || len(backend.endRuns) != 0 {
		t.Errorf("BeginRun called %d times and EndRun %d, want neither", backend.beginRuns, len(backend.endRuns))
	}
}
