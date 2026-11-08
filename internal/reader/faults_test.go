package reader

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/dbtest"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/validate"
	"github.com/colligo-go/scribo/internal/writer"
)

func ignore(exitcode.Warning) {}

func readable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "study.db")
	handle, err := sql.Open("sqlite", "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.Exec(`CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, reads INTEGER, UNIQUE(sample, replicate))`); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1", "2"} {
		err := writer.Upsert(context.Background(), sqlite.New(), writer.Request{
			DB: path, Table: "rnaseq_qc",
			Key:    db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}},
			Values: db.Values{"reads": {Raw: value}},
		}, ignore)
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func sweepReads(t *testing.T, read func(backend db.Backend) (int, error)) {
	t.Helper()
	want, err := read(sqlite.New())
	if err != nil || want == 0 {
		t.Fatalf("without failures: %d results, error %v", want, err)
	}
	for failAt := 1; ; failAt++ {
		backend := &dbtest.Failing{Backend: sqlite.New(), FailAt: failAt}

		got, err := read(backend)

		if !backend.Reached() {
			if err != nil || got != want {
				t.Fatalf("no failure injected: %d results, error %v", got, err)
			}
			if failAt < 4 {
				t.Fatalf("the read made only %d backend calls", failAt-1)
			}
			return
		}
		if !errors.Is(err, dbtest.ErrInjected) {
			t.Errorf("call %d (%s) failed: error %v is not the injected failure", failAt, backend.Failed, err)
		}
	}
}

func TestEveryBackendFailureOfAQueryIsReturned(t *testing.T) {
	path := readable(t)
	sweepReads(t, func(backend db.Backend) (int, error) {
		result, err := Query(context.Background(), backend, QueryRequest{
			DB: path, Table: "rnaseq_qc", Filters: validate.Filters{Where: []string{"reads>0"}},
		}, ignore)
		return len(result.Rows), err
	})
}

func TestEveryBackendFailureOfALogIsReturned(t *testing.T) {
	path := readable(t)
	key := db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}}
	for name, request := range map[string]LogRequest{
		"history with runs": {DB: path, Table: "rnaseq_qc", Key: key, WithRuns: true},
		"state at a moment": {DB: path, Table: "rnaseq_qc", Key: key, At: "2999-01-01T00:00:00.000Z"},
		"state after a run": {DB: path, Table: "rnaseq_qc", Key: key, AtRun: 2},
	} {
		t.Run(name, func(t *testing.T) {
			sweepReads(t, func(backend db.Backend) (int, error) {
				result, err := Log(context.Background(), backend, request, ignore)
				return len(result.Rows) + len(result.State), err
			})
		})
	}
}
