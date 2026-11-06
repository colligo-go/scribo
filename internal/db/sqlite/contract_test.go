package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/contract"
	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func rawDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "study.db")
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })
	return path, handle
}

func schemaSnapshot(t *testing.T, path string) string {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	var snapshot sql.NullString
	if err := handle.QueryRow("SELECT group_concat(sql, ';') FROM sqlite_schema").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot.String
}

func rowCount(t *testing.T, path, table string) int {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	var count int
	if err := handle.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestEnsureContractTablesCreatesStrictToolRuns(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})

	if err := backend.EnsureContractTables(context.Background()); err != nil {
		t.Fatal(err)
	}

	exists, err := backend.TableExists(context.Background(), "tool_runs")
	if err != nil || !exists {
		t.Fatalf("TableExists(tool_runs) = %v, %v", exists, err)
	}
	var strict int
	if err := backend.db.QueryRow(`SELECT "strict" FROM pragma_table_list('tool_runs')`).Scan(&strict); err != nil {
		t.Fatal(err)
	}
	if strict != 1 {
		t.Errorf("strict = %d, want 1", strict)
	}
}

func TestEnsureContractTablesIsIdempotent(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})

	for i := 0; i < 2; i++ {
		if err := backend.EnsureContractTables(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
}

func colligoRows(t *testing.T, handle *sql.DB) string {
	t.Helper()
	rows, err := handle.Query("SELECT * FROM tool_runs WHERE tool = 'colligo' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "%v\n", values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestColligoToolRunsIsAcceptedAndUntouched(t *testing.T) {
	path, handle := rawDatabase(t)
	if _, err := handle.Exec(contract.ToolRunsDDL); err != nil {
		t.Fatal(err)
	}
	_, err := handle.Exec(`INSERT INTO tool_runs
		(contract_version, tool, tool_version, git_sha, go_version, host, step, config_hash, argv_shape, started_at, ended_at, exit_code)
		VALUES (1, 'colligo', 'v0.3.0', 'abc123', 'go1.26.3', 'node1', NULL, 'cfg', '{"command":"colligo add"}', '2026-11-06T10:00:00.000Z', '2026-11-06T10:00:01.000Z', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	before := colligoRows(t, handle)
	handle.Close()

	backend := openBackend(t, path, localFS, db.OpenOptions{})
	ctx := context.Background()
	if err := backend.EnsureContractTables(ctx); err != nil {
		t.Fatal(err)
	}
	runID, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.EndRun(ctx, runID, 0); err != nil {
		t.Fatal(err)
	}

	if after := colligoRows(t, backend.db); after != before {
		t.Errorf("Colligo's row changed:\nbefore %safter  %s", before, after)
	}
}

func TestDivergentToolRunsIsRefused(t *testing.T) {
	ddl := contract.ToolRunsDDL
	variants := []struct {
		name    string
		ddl     string
		mention string
	}{
		{"missing column", strings.Replace(ddl, "    step             TEXT,\n", "", 1), "step"},
		{"extra column", strings.Replace(ddl, "    exit_code        INTEGER\n", "    exit_code        INTEGER,\n    extra            TEXT\n", 1), "extra"},
		{"wrong column type", strings.Replace(ddl, "exit_code        INTEGER", "exit_code        TEXT", 1), "exit_code is TEXT, expected INTEGER"},
		{"lost NOT NULL", strings.Replace(ddl, "tool_version     TEXT NOT NULL", "tool_version     TEXT", 1), "tool_version"},
		{"not strict", strings.Replace(ddl, ") STRICT;", ");", 1), "STRICT"},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			if variant.ddl == ddl {
				t.Fatal("variant is identical to the contract DDL")
			}
			path, handle := rawDatabase(t)
			if _, err := handle.Exec(variant.ddl); err != nil {
				t.Fatal(err)
			}
			handle.Close()
			before := schemaSnapshot(t, path)

			backend := New()
			backend.detect = localFS
			err := backend.Open(context.Background(), path, db.OpenOptions{})
			backend.Close()

			var domain *exitcode.Error
			if !errors.As(err, &domain) {
				t.Fatalf("Open error = %v, want an *exitcode.Error", err)
			}
			if domain.Code != "contract_mismatch" || domain.Status != exitcode.Database {
				t.Errorf("got status %d code %q, want %d contract_mismatch", domain.Status, domain.Code, exitcode.Database)
			}
			if !strings.Contains(domain.Message, variant.mention) {
				t.Errorf("message %q does not mention %q", domain.Message, variant.mention)
			}
			if after := schemaSnapshot(t, path); after != before {
				t.Errorf("schema changed:\nbefore %s\nafter  %s", before, after)
			}
			if count := rowCount(t, path, "tool_runs"); count != 0 {
				t.Errorf("tool_runs has %d rows, want 0", count)
			}
		})
	}
}
