package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/colligo-go/scribo/internal/contract"
	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func newRunBackend(t *testing.T) *Backend {
	t.Helper()
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})
	if err := backend.EnsureContractTables(context.Background()); err != nil {
		t.Fatal(err)
	}
	return backend
}

func fixedNow(t *testing.T, stamp string) {
	t.Helper()
	original := now
	now = func() string { return stamp }
	t.Cleanup(func() { now = original })
}

type runRow struct {
	contractVersion int
	tool            string
	startedAt       string
	endedAt         sql.NullString
	exitCode        sql.NullInt64
	step            sql.NullString
	host            sql.NullString
}

func readRun(t *testing.T, backend *Backend, id int64) runRow {
	t.Helper()
	var row runRow
	err := backend.db.QueryRow(
		`SELECT contract_version, tool, started_at, ended_at, exit_code, step, host FROM tool_runs WHERE id = ?`, id).
		Scan(&row.contractVersion, &row.tool, &row.startedAt, &row.endedAt, &row.exitCode, &row.step, &row.host)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestBeginRun(t *testing.T) {
	backend := newRunBackend(t)
	fixedNow(t, "2026-11-06T12:00:00.123Z")

	id, err := backend.BeginRun(context.Background(), db.RunMeta{
		ToolVersion: "v0.1.0", GitSHA: "abc", GoVersion: "go1.26.3", ArgvShape: `{"command":"scribo x","flags":[],"args":0}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	row := readRun(t, backend, id)
	if row.contractVersion != contract.Version || row.tool != "scribo" {
		t.Errorf("contract_version/tool = %d/%q, want 1/scribo", row.contractVersion, row.tool)
	}
	if row.startedAt != "2026-11-06T12:00:00.123Z" {
		t.Errorf("started_at = %q", row.startedAt)
	}
	if row.endedAt.Valid || row.exitCode.Valid {
		t.Errorf("ended_at/exit_code = %v/%v, want NULL", row.endedAt, row.exitCode)
	}
	if row.step.Valid || row.host.Valid {
		t.Errorf("step/host = %v/%v, want NULL for empty strings", row.step, row.host)
	}
}

func TestBeginRunStoresNonEmptyOptionalFields(t *testing.T) {
	backend := newRunBackend(t)

	id, err := backend.BeginRun(context.Background(), db.RunMeta{
		ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}", Step: "align", Host: "node1",
	})
	if err != nil {
		t.Fatal(err)
	}

	row := readRun(t, backend, id)
	if row.step.String != "align" || row.host.String != "node1" {
		t.Errorf("step/host = %v/%v", row.step, row.host)
	}
}

func TestTimestampFormat(t *testing.T) {
	backend := newRunBackend(t)

	id, err := backend.BeginRun(context.Background(), db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}

	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	if started := readRun(t, backend, id).startedAt; !pattern.MatchString(started) {
		t.Errorf("started_at = %q, want RFC 3339 UTC with milliseconds", started)
	}
}

func TestEndRun(t *testing.T) {
	backend := newRunBackend(t)
	ctx := context.Background()
	id, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	fixedNow(t, "2026-11-06T12:00:05.500Z")

	if err := backend.EndRun(ctx, id, 3); err != nil {
		t.Fatal(err)
	}

	row := readRun(t, backend, id)
	if row.endedAt.String != "2026-11-06T12:00:05.500Z" || row.exitCode.Int64 != 3 {
		t.Errorf("ended_at/exit_code = %v/%v", row.endedAt, row.exitCode)
	}
}

func requireInternal(t *testing.T, err error) {
	t.Helper()
	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("error = %v, want an *exitcode.Error", err)
	}
	if domain.Code != "internal" || domain.Status != exitcode.System {
		t.Errorf("got status %d code %q, want %d internal", domain.Status, domain.Code, exitcode.System)
	}
}

func TestEndRunTwice(t *testing.T) {
	backend := newRunBackend(t)
	ctx := context.Background()
	id, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.EndRun(ctx, id, 0); err != nil {
		t.Fatal(err)
	}

	requireInternal(t, backend.EndRun(ctx, id, 1))

	if row := readRun(t, backend, id); row.exitCode.Int64 != 0 {
		t.Errorf("second EndRun overwrote exit_code: %v", row.exitCode)
	}
}

func TestEndRunRefusesColligoRow(t *testing.T) {
	path, handle := rawDatabase(t)
	if _, err := handle.Exec(contract.ToolRunsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO tool_runs
		(contract_version, tool, tool_version, git_sha, go_version, argv_shape, started_at)
		VALUES (1, 'colligo', 'v0.3.0', 'abc', 'go1.26.3', '{}', '2026-11-06T10:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	handle.Close()
	backend := openBackend(t, path, localFS, db.OpenOptions{})

	requireInternal(t, backend.EndRun(context.Background(), 1, 0))

	row := readRun(t, backend, 1)
	if row.endedAt.Valid || row.exitCode.Valid {
		t.Errorf("Colligo's row was closed by Scribo: %v/%v", row.endedAt, row.exitCode)
	}
}
