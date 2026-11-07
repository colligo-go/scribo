package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const qcTable = `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, Alignment_Rate REAL,
	Mapped_Reads INTEGER, note TEXT, run_date DATETIME, UNIQUE(sample, replicate))`

func openRaw(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path+"?_busy_timeout=10000")
}

func newDB(t *testing.T, ddl ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "study.db")
	handle, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	for _, statement := range ddl {
		if _, err := handle.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	return path
}

func query(t *testing.T, path, statement string, args ...any) [][]string {
	t.Helper()
	handle, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	rows, err := handle.Query(statement, args...)
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		line := make([]string, len(columns))
		for i, cell := range cells {
			switch v := cell.(type) {
			case nil:
				line[i] = "NULL"
			case []byte:
				line[i] = string(v)
			default:
				line[i] = fmt.Sprint(v)
			}
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func scalar(t *testing.T, path, statement string, args ...any) string {
	t.Helper()
	rows := query(t, path, statement, args...)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%s returned %v, want one cell", statement, rows)
	}
	return rows[0][0]
}

func count(t *testing.T, path, table string) string {
	t.Helper()
	return scalar(t, path, "SELECT count(*) FROM "+table)
}

func upsert(t *testing.T, path string, args ...string) result {
	t.Helper()
	base := []string{"upsert", "--db", path, "--table", "rnaseq_qc"}
	return runScribo(t, append(base, args...)...)
}

func upsertS1(t *testing.T, path string, args ...string) result {
	t.Helper()
	keys := []string{"--key", "sample=S1", "--key", "replicate=1"}
	return upsert(t, path, append(keys, args...)...)
}

func requireSuccess(t *testing.T, got result) {
	t.Helper()
	if got.ExitCode != 0 || got.Stdout != "" || got.Stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q, want a silent success", got.ExitCode, got.Stdout, got.Stderr)
	}
}

func requireFailure(t *testing.T, got result, exit int, code string) {
	t.Helper()
	if got.ExitCode != exit || !strings.Contains(got.Stderr, "ERROR ["+code+"]") || got.Stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q, want exit %d [%s]", got.ExitCode, got.Stdout, got.Stderr, exit, code)
	}
}

func seeded(t *testing.T) string {
	t.Helper()
	path := newDB(t, qcTable)
	requireSuccess(t, upsertS1(t, path, "Alignment_Rate=94.3", "Mapped_Reads=12", "note=ok"))
	return path
}

func snapshot(t *testing.T, path string, tables ...string) string {
	t.Helper()
	var out strings.Builder
	for _, table := range tables {
		fmt.Fprintf(&out, "%s: %v\n", table, query(t, path, "SELECT * FROM "+table+" ORDER BY 1, 2"))
	}
	return out.String()
}

func TestUpsertNewRecordWritesHistoryAndRun(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	requireSuccess(t, upsertS1(t, path, "Alignment_Rate=94.3", "Mapped_Reads=12", "note=ok"))

	if got := count(t, path, "rnaseq_qc"); got != "1" {
		t.Errorf("data rows = %s, want 1", got)
	}
	rows := query(t, path, "SELECT field, old_value, old_is_null, new_value, new_is_null, source, record_key FROM rnaseq_qc_history ORDER BY field")
	want := [][]string{
		{"Alignment_Rate", "NULL", "1", "94.3", "0", "scribo", `[["replicate","1"],["sample","S1"]]`},
		{"Mapped_Reads", "NULL", "1", "12", "0", "scribo", `[["replicate","1"],["sample","S1"]]`},
		{"note", "NULL", "1", "ok", "0", "scribo", `[["replicate","1"],["sample","S1"]]`},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Errorf("history = %v, want %v", rows, want)
	}
	runs := query(t, path, "SELECT id, tool, exit_code, ended_at IS NOT NULL FROM tool_runs")
	if len(runs) != 1 || runs[0][1] != "scribo" || runs[0][2] != "0" || runs[0][3] != "1" {
		t.Fatalf("tool_runs = %v", runs)
	}
	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE run_id = ?", runs[0][0]); got != "3" {
		t.Errorf("history rows under the run = %s, want 3", got)
	}
}

func TestUpsertRecordsOnlyTheFieldThatChanged(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireSuccess(t, upsertS1(t, path, "Alignment_Rate=94.3", "Mapped_Reads=13"))

	rows := query(t, path, "SELECT field, old_value, new_value FROM rnaseq_qc_history WHERE id > 3")
	if fmt.Sprint(rows) != "[[Mapped_Reads 12 13]]" {
		t.Errorf("new history rows = %v", rows)
	}
}

func TestUpsertRepeatedWriteAddsRunButNoHistory(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireSuccess(t, upsertS1(t, path, "Alignment_Rate=94.3", "Mapped_Reads=12", "note=ok"))

	if got := count(t, path, "rnaseq_qc_history"); got != "3" {
		t.Errorf("history rows = %s, want 3", got)
	}
	if got := scalar(t, path, "SELECT count(*) FROM tool_runs WHERE exit_code = 0"); got != "2" {
		t.Errorf("successful runs = %s, want 2", got)
	}
}

func TestUpsertSameNumberWrittenDifferentlyIsNoChange(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireSuccess(t, upsertS1(t, path, "Alignment_Rate=94.30"))

	if got := count(t, path, "rnaseq_qc_history"); got != "3" {
		t.Errorf("history rows = %s, want 3", got)
	}
}

func TestUpsertKeyOrderAndNumberSpellingDoNotSplitTheRecord(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	requireSuccess(t, upsert(t, path, "--key", "replicate=01", "--key", "sample=S1", "Mapped_Reads=14"))

	if got := count(t, path, "rnaseq_qc"); got != "1" {
		t.Errorf("data rows = %s, want 1", got)
	}
	if got := scalar(t, path, "SELECT count(DISTINCT record_key) FROM rnaseq_qc_history"); got != "1" {
		t.Errorf("distinct record keys = %s, want 1", got)
	}
}

func TestUpsertNullHasItsOwnSyntax(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	last := "SELECT typeof(new_value), new_value, new_is_null FROM rnaseq_qc_history WHERE field = 'note' ORDER BY id DESC LIMIT 1"
	stored := "SELECT typeof(note), ifnull(note, 'NULL') FROM rnaseq_qc"

	requireSuccess(t, upsertS1(t, path, "note=null"))
	if got := fmt.Sprint(query(t, path, last), query(t, path, stored)); got != "[[text null 0]] [[text null]]" {
		t.Errorf("after note=null: %s", got)
	}

	requireSuccess(t, upsertS1(t, path, "--null", "note"))
	if got := fmt.Sprint(query(t, path, last), query(t, path, stored)); got != "[[null NULL 1]] [[null NULL]]" {
		t.Errorf("after --null note: %s", got)
	}

	requireSuccess(t, upsertS1(t, path, "note="))
	if got := fmt.Sprint(query(t, path, last), query(t, path, stored)); got != "[[text  0]] [[text ]]" {
		t.Errorf("after note=: %s", got)
	}
}

func TestUpsertSplitsAtTheFirstEqualsAndKeepsCommas(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	requireSuccess(t, upsert(t, path, "--key", "sample=a,b", "--key", "replicate=1", "note=a=b"))

	if got := fmt.Sprint(query(t, path, "SELECT sample, note FROM rnaseq_qc")); got != "[[a,b a=b]]" {
		t.Errorf("row = %s", got)
	}
}

func TestUpsertKeepsDatetimeTextAndWritesItOnce(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)
	const stamp = "2026-11-06 12:00:00"

	requireSuccess(t, upsertS1(t, path, "run_date="+stamp))
	requireSuccess(t, upsertS1(t, path, "run_date="+stamp))

	if got := scalar(t, path, "SELECT +run_date FROM rnaseq_qc"); got != stamp {
		t.Errorf("stored run_date = %q", got)
	}
	if got := scalar(t, path, "SELECT new_value FROM rnaseq_qc_history"); got != stamp {
		t.Errorf("history new_value = %q", got)
	}
	if got := count(t, path, "rnaseq_qc_history"); got != "1" {
		t.Errorf("history rows = %s, want 1", got)
	}
}

func TestUpsertInvalidValuesWriteNothingAtAll(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	tables := []string{"rnaseq_qc", "rnaseq_qc_history", "tool_runs"}
	before := snapshot(t, path, tables...)

	for _, field := range []string{
		"Mapped_Reads=94.3",
		"Alignment_Rate=abc",
		"Alignment_Rate=NaN",
		"Mapped_Reads=9223372036854775808",
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			requireFailure(t, upsertS1(t, path, field), 3, "invalid_value")
			if after := snapshot(t, path, tables...); after != before {
				t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestUpsertUnknownTableAndColumn(t *testing.T) {
	t.Parallel()
	path := seeded(t)

	got := runScribo(t, "upsert", "--db", path, "--table", "nope", "--key", "sample=S1", "note=x")
	requireFailure(t, got, 2, "table_not_found")

	got = upsertS1(t, path, "Alignment_rate=1")
	requireFailure(t, got, 2, "column_not_found")
	if !strings.Contains(got.Stderr, `did you mean "Alignment_Rate"`) {
		t.Errorf("stderr = %q, want a suggestion", got.Stderr)
	}
}

func TestUpsertRefusesReservedTables(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	tables := []string{"rnaseq_qc", "rnaseq_qc_history", "tool_runs"}
	before := snapshot(t, path, tables...)
	schema := scalar(t, path, "SELECT group_concat(sql, ';') FROM sqlite_schema")

	t.Run("each table", func(t *testing.T) {
		for _, table := range []string{
			"tool_runs", "samples", "files", "facts", "result_inputs", "input_signals",
			"rnaseq_qc_history", "sqlite_master", "TOOL_RUNS",
		} {
			t.Run(table, func(t *testing.T) {
				t.Parallel()
				got := runScribo(t, "upsert", "--db", path, "--table", table, "--key", "id=1", "x=1")
				requireFailure(t, got, 2, "reserved_table")
			})
		}
	})
	if after := snapshot(t, path, tables...); after != before {
		t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
	}
	if after := scalar(t, path, "SELECT group_concat(sql, ';') FROM sqlite_schema"); after != schema {
		t.Error("schema changed")
	}
}

func TestUpsertUsageErrorsAreCaughtBeforeTheDatabaseIsOpened(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)
	db := []string{"--db", path}
	table := []string{"--table", "rnaseq_qc"}
	key := []string{"--key", "sample=S1"}
	join := func(parts ...[]string) []string {
		out := []string{"upsert"}
		for _, part := range parts {
			out = append(out, part...)
		}
		return out
	}
	cases := map[string][]string{
		"no --db":                join(table, key, []string{"x=1"}),
		"no --table":             join(db, key, []string{"x=1"}),
		"no --key":               join(db, table, []string{"x=1"}),
		"no fields":              join(db, table, key),
		"positional without =":   join(db, table, key, []string{"novalue"}),
		"empty field name":       join(db, table, key, []string{"=3"}),
		"repeated field":         join(db, table, key, []string{"x=1", "x=2"}),
		"field and --null":       join(db, table, key, []string{"x=1", "--null", "x"}),
		"field is a key":         join(db, table, key, []string{"sample=S2"}),
		"null on a key":          join(db, table, key, []string{"--null", "sample"}),
		"repeated key":           join(db, table, key, []string{"--key", "sample=S2", "x=1"}),
		"key without =":          join(db, table, []string{"--key", "sample", "x=1"}),
		"busy timeout zero":      join(db, table, key, []string{"--busy-timeout", "0", "x=1"}),
		"busy timeout negative":  join(db, table, key, []string{"--busy-timeout=-5", "x=1"}),
		"insert and update only": join(db, table, key, []string{"--insert-only", "--update-only", "x=1"}),
		"empty --null name":      join(db, table, key, []string{"--null", ""}),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireFailure(t, runScribo(t, args...), 1, "usage")
			if got := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name = 'tool_runs'"); got != "0" {
				t.Error("a usage error touched the database")
			}
		})
	}
}

func TestUpsertModesRecordTheFailedRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		flag string
		code string
	}{
		{"insert-only on an existing record", "--insert-only", "key_already_exists"},
		{"update-only on a missing record", "--update-only", "key_not_found"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := seeded(t)
			args := []string{tt.flag, "Mapped_Reads=99"}
			if tt.flag == "--update-only" {
				args = []string{tt.flag, "--key", "sample=S2", "--key", "replicate=1", "Mapped_Reads=99"}
				requireFailure(t, upsert(t, path, args...), 3, tt.code)
			} else {
				requireFailure(t, upsertS1(t, path, args...), 3, tt.code)
			}
			runs := query(t, path, "SELECT exit_code, ended_at IS NOT NULL FROM tool_runs ORDER BY id DESC LIMIT 1")
			if fmt.Sprint(runs) != "[[3 1]]" {
				t.Errorf("last run = %v, want exit_code 3 with ended_at", runs)
			}
			if got := count(t, path, "tool_runs"); got != "2" {
				t.Errorf("tool_runs rows = %s, want 2", got)
			}
			if got := count(t, path, "rnaseq_qc_history"); got != "3" {
				t.Errorf("history rows = %s, want 3", got)
			}
			if got := count(t, path, "rnaseq_qc"); got != "1" {
				t.Errorf("data rows = %s, want 1", got)
			}
		})
	}
}

func requireOneFailedRun(t *testing.T, path string, before int) {
	t.Helper()
	runs := query(t, path, "SELECT exit_code, ended_at IS NOT NULL FROM tool_runs ORDER BY id")
	if len(runs) != before+1 || fmt.Sprint(runs[len(runs)-1]) != "[3 1]" {
		t.Errorf("tool_runs = %v, want %d rows and the last with exit_code 3 and ended_at", runs, before+1)
	}
}

func TestUpsertConstraintFailureIsTypedAndRecorded(t *testing.T) {
	t.Parallel()
	path := newDB(t, `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, x TEXT NOT NULL, y INTEGER, UNIQUE(sample, replicate))`)

	requireFailure(t, upsertS1(t, path, "y=1"), 3, "constraint_failed")

	if got := count(t, path, "rnaseq_qc"); got != "0" {
		t.Errorf("data rows = %s, want 0", got)
	}
	requireOneFailedRun(t, path, 0)
}

func TestUpsertIsAtomicWhenTheHistoryWriteFails(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	if _, err := execRaw(path, `CREATE TRIGGER boom AFTER INSERT ON rnaseq_qc_history BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	tables := []string{"rnaseq_qc", "rnaseq_qc_history"}
	before := snapshot(t, path, tables...)

	requireFailure(t, upsertS1(t, path, "Mapped_Reads=500"), 3, "constraint_failed")

	if after := snapshot(t, path, tables...); after != before {
		t.Errorf("a failed history write left changes behind:\nbefore %s\nafter  %s", before, after)
	}
	if got := scalar(t, path, "SELECT Mapped_Reads FROM rnaseq_qc"); got != "12" {
		t.Errorf("Mapped_Reads = %s, want the old value 12", got)
	}
	requireOneFailedRun(t, path, 1)
}

func execRaw(path string, statement string) (sql.Result, error) {
	handle, err := openRaw(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return handle.Exec(statement)
}

func TestUpsertWorksOnStrictTables(t *testing.T) {
	t.Parallel()
	path := newDB(t, `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, n INTEGER, r REAL, UNIQUE(sample, replicate)) STRICT`)

	requireSuccess(t, upsertS1(t, path, "n=5", "r=3"))

	if got := fmt.Sprint(query(t, path, "SELECT typeof(n), typeof(r), typeof(replicate) FROM rnaseq_qc")); got != "[[integer real integer]]" {
		t.Errorf("types = %s", got)
	}
}

func TestUpsertReportsALockedDatabase(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	holder, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	got := upsertS1(t, path, "--busy-timeout", "200", "Mapped_Reads=500")
	elapsed := time.Since(start)
	conn.ExecContext(context.Background(), "ROLLBACK")

	requireFailure(t, got, 5, "db_busy")
	if elapsed > 2*time.Second {
		t.Errorf("took %v to give up", elapsed)
	}
	if got := scalar(t, path, "SELECT Mapped_Reads FROM rnaseq_qc"); got != "12" {
		t.Errorf("Mapped_Reads = %s, want 12", got)
	}
	if got := count(t, path, "tool_runs"); got != "1" {
		t.Errorf("tool_runs rows = %s, want 1", got)
	}
}

func TestUpsertLeaksNothingFromItsArguments(t *testing.T) {
	t.Parallel()
	const sentinel = "LEAK-7f3a9c"
	dir := filepath.Join(t.TempDir(), sentinel)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "study.db")
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(qcTable); err != nil {
		t.Fatal(err)
	}
	handle.Close()

	got := runScribo(t, "upsert", "--db", path, "--table", "rnaseq_qc",
		"--key", "sample="+sentinel, "--key", "replicate=1", "note="+sentinel)

	requireSuccess(t, got)
	recorded := scalar(t, path, `SELECT argv_shape || tool_version || git_sha || go_version || ifnull(host, '') || ifnull(step, '') FROM tool_runs`)
	if strings.Contains(recorded, sentinel) {
		t.Errorf("tool_runs leaks the sentinel: %s", recorded)
	}
}

func TestUpsertHelp(t *testing.T) {
	t.Parallel()
	got := runScribo(t, "upsert", "--help")

	if got.ExitCode != 0 {
		t.Fatalf("exit %d, stderr %q", got.ExitCode, got.Stderr)
	}
	for _, want := range []string{"--key", "--null", "--update-only", "--insert-only", "Alignment_Rate=94.3"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}
