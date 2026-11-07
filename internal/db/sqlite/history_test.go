package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

const qcDDL = `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, Alignment_Rate REAL, Mapped_Reads INTEGER, note TEXT, run_date DATETIME, UNIQUE(sample, replicate))`

func newQCBackend(t *testing.T, ddl string) *Backend {
	t.Helper()
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})
	if _, err := backend.db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := backend.EnsureContractTables(ctx); err != nil {
		t.Fatal(err)
	}
	return backend
}

func s1() db.Key {
	return db.Key{"sample": {Raw: "S1"}, "replicate": {Raw: "1"}}
}

func upsertOnce(t *testing.T, backend *Backend, table string, key db.Key, values db.Values, mode db.WriteMode) ([]db.FieldChange, error) {
	t.Helper()
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	changes, err := tx.Upsert(context.Background(), table, key, values, mode)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return changes, nil
}

func TestEnsureHistoryTable(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := backend.EnsureHistoryTable(ctx, "rnaseq_qc"); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	var strict, indexes int
	if err := backend.db.QueryRow(`SELECT "strict" FROM pragma_table_list('rnaseq_qc_history')`).Scan(&strict); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND tbl_name = 'rnaseq_qc_history' AND name LIKE 'idx_%'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if strict != 1 || indexes != 3 {
		t.Errorf("strict = %d, indexes = %d, want 1 and 3", strict, indexes)
	}
}

func TestTableNameWithQuoteCharacter(t *testing.T) {
	backend := newQCBackend(t, `CREATE TABLE "qc""x" (k TEXT, v INTEGER, UNIQUE(k))`)
	ctx := context.Background()
	table := `qc"x`

	if err := backend.EnsureHistoryTable(ctx, table); err != nil {
		t.Fatal(err)
	}
	runID, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	changes, err := tx.Upsert(ctx, table, db.Key{"k": {Raw: "a"}}, db.Values{"v": {Raw: "7"}}, db.ModeUpsert)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendHistory(ctx, table, `[["k","a"]]`, changes, runID, time.Now(), db.SourceScribo); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := backend.db.QueryRow(`SELECT count(*) FROM "qc""x_history"`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("history rows = %d, err %v, want 1", rows, err)
	}
}

func TestDivergentHistoryTableIsRefused(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	if _, err := backend.db.Exec(`CREATE TABLE rnaseq_qc_history (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := backend.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, path)

	err := backend.EnsureHistoryTable(context.Background(), "rnaseq_qc")

	domain := requireCode(t, err, exitcode.Database, "contract_mismatch")
	if domain != nil && !strings.Contains(domain.Message, "SPEC §4") {
		t.Errorf("message %q does not cite SPEC §4", domain.Message)
	}
	if after := schemaSnapshot(t, path); after != before {
		t.Errorf("schema changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestBackendRefusesUseWhileTransactionIsOpen(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err = backend.TableExists(ctx, "rnaseq_qc")

	requireCode(t, err, exitcode.System, "internal")
	if time.Since(start) > time.Second {
		t.Error("TableExists did not return at once")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.TableExists(context.Background(), "rnaseq_qc"); err != nil {
		t.Errorf("Backend still locked after Commit: %v", err)
	}
}

func TestSelectRowReturnsDatetimeTextUntouched(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	if _, err := backend.db.Exec(`INSERT INTO rnaseq_qc (sample, replicate, run_date) VALUES ('S1', 1, '2026-11-06 12:00:00')`); err != nil {
		t.Fatal(err)
	}
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	row, found, err := tx.SelectRow(context.Background(), "rnaseq_qc", s1(), []string{"run_date"})

	if err != nil || !found {
		t.Fatalf("SelectRow = %v, %v, %v", row, found, err)
	}
	if got := row["run_date"]; got != (db.Value{Raw: "2026-11-06 12:00:00"}) {
		t.Errorf("run_date = %+v, want the exact stored text", got)
	}
}

func TestUpsertNewRecordReportsEveryFieldInOrder(t *testing.T) {
	backend := newQCBackend(t, qcDDL)

	changes, err := upsertOnce(t, backend, "rnaseq_qc", s1(), db.Values{
		"note":           {Raw: "ok"},
		"Mapped_Reads":   {Raw: "12"},
		"Alignment_Rate": {Raw: "94.3"},
	}, db.ModeUpsert)
	if err != nil {
		t.Fatal(err)
	}

	want := []db.FieldChange{
		{Field: "Alignment_Rate", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: "94.3"}},
		{Field: "Mapped_Reads", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: "12"}},
		{Field: "note", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: "ok"}},
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want %+v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, changes[i], want[i])
		}
	}
}

func TestUpsertReportsOnlyWhatChanged(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	if _, err := upsertOnce(t, backend, "rnaseq_qc", s1(), db.Values{
		"Alignment_Rate": {Raw: "94.3"}, "Mapped_Reads": {Raw: "12"},
	}, db.ModeUpsert); err != nil {
		t.Fatal(err)
	}

	changes, err := upsertOnce(t, backend, "rnaseq_qc", s1(), db.Values{
		"Alignment_Rate": {Raw: "94.30"}, "Mapped_Reads": {Raw: "13"},
	}, db.ModeUpsert)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 || changes[0].Field != "Mapped_Reads" ||
		changes[0].OldValue.Raw != "12" || changes[0].NewValue.Raw != "13" {
		t.Errorf("changes = %+v, want only Mapped_Reads 12 -> 13", changes)
	}
}

func TestUpsertSameNumberWrittenDifferentlyIsNoChange(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	values := func(raw string) db.Values { return db.Values{"Alignment_Rate": {Raw: raw}} }
	if _, err := upsertOnce(t, backend, "rnaseq_qc", s1(), values("94.3"), db.ModeUpsert); err != nil {
		t.Fatal(err)
	}

	changes, err := upsertOnce(t, backend, "rnaseq_qc", s1(), values("94.30"), db.ModeUpsert)

	if err != nil || len(changes) != 0 {
		t.Errorf("changes = %+v, err %v, want none", changes, err)
	}
}

func TestUpsertAmbiguousKey(t *testing.T) {
	backend := newQCBackend(t, `CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, note TEXT)`)
	for i := 0; i < 2; i++ {
		if _, err := backend.db.Exec(`INSERT INTO rnaseq_qc VALUES ('S1', 1, 'a')`); err != nil {
			t.Fatal(err)
		}
	}

	_, err := upsertOnce(t, backend, "rnaseq_qc", s1(), db.Values{"note": {Raw: "b"}}, db.ModeUpsert)

	requireCode(t, err, exitcode.Data, "ambiguous_key")
	var notes int
	if err := backend.db.QueryRow(`SELECT count(*) FROM rnaseq_qc WHERE note = 'a'`).Scan(&notes); err != nil || notes != 2 {
		t.Errorf("rows with the old note = %d, err %v, want 2", notes, err)
	}
}

func TestUpsertModes(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	values := db.Values{"note": {Raw: "x"}}

	_, err := upsertOnce(t, backend, "rnaseq_qc", s1(), values, db.ModeUpdateOnly)
	requireCode(t, err, exitcode.Data, "key_not_found")

	if _, err := upsertOnce(t, backend, "rnaseq_qc", s1(), values, db.ModeInsertOnly); err != nil {
		t.Fatalf("insert-only on a missing record: %v", err)
	}
	_, err = upsertOnce(t, backend, "rnaseq_qc", s1(), values, db.ModeInsertOnly)
	requireCode(t, err, exitcode.Data, "key_already_exists")
}

func TestUpsertUnknownColumn(t *testing.T) {
	backend := newQCBackend(t, qcDDL)

	_, err := upsertOnce(t, backend, "rnaseq_qc", s1(), db.Values{"nope": {Raw: "x"}}, db.ModeUpsert)

	requireCode(t, err, exitcode.Database, "column_not_found")
}

func TestRollbackAfterCommitIsNil(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Errorf("Rollback after Commit = %v, want nil", err)
	}
}

func TestRollbackUndoesTheUpsert(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Upsert(context.Background(), "rnaseq_qc", s1(), db.Values{"note": {Raw: "x"}}, db.ModeUpsert); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := backend.db.QueryRow(`SELECT count(*) FROM rnaseq_qc`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rows = %d, err %v, want 0", rows, err)
	}
}

func TestAppendHistoryRows(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()
	if err := backend.EnsureHistoryTable(ctx, "rnaseq_qc"); err != nil {
		t.Fatal(err)
	}
	runID, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Date(2026, 11, 6, 12, 0, 0, 123_000_000, time.UTC)
	err = tx.AppendHistory(ctx, "rnaseq_qc", `[["sample","S1"]]`, []db.FieldChange{
		{Field: "note", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: ""}},
		{Field: "Mapped_Reads", OldValue: db.Value{Raw: "1"}, NewValue: db.Value{IsNull: true}},
	}, runID, at, db.SourceScribo)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rows, err := backend.db.Query(`SELECT field, old_value, old_is_null, new_value, new_is_null, source, changed_at FROM rnaseq_qc_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		field, source, changedAt string
		oldValue, newValue       *string
		oldNull, newNull         int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.field, &r.oldValue, &r.oldNull, &r.newValue, &r.newNull, &r.source, &r.changedAt); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows", len(got))
	}
	if got[0].oldValue != nil || got[0].oldNull != 1 || got[0].newValue == nil || *got[0].newValue != "" || got[0].newNull != 0 {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].newValue != nil || got[1].newNull != 1 || got[1].oldValue == nil || *got[1].oldValue != "1" {
		t.Errorf("row 1 = %+v", got[1])
	}
	if got[0].source != "scribo" || got[0].changedAt != "2026-11-06T12:00:00.123Z" {
		t.Errorf("row 0 = %+v", got[0])
	}
}

func historyFixture(t *testing.T) (*Backend, int64) {
	t.Helper()
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()
	if err := backend.EnsureHistoryTable(ctx, "rnaseq_qc"); err != nil {
		t.Fatal(err)
	}
	runID, err := backend.BeginRun(ctx, db.RunMeta{ToolVersion: "v", GitSHA: "g", GoVersion: "go", ArgvShape: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	return backend, runID
}

func appendOne(t *testing.T, backend *Backend, recordKey string, runID int64, at time.Time) {
	t.Helper()
	tx, err := backend.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	change := []db.FieldChange{{Field: "note", OldValue: db.Value{IsNull: true}, NewValue: db.Value{Raw: "x"}}}
	if err := tx.AppendHistory(context.Background(), "rnaseq_qc", recordKey, change, runID, at, db.SourceScribo); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestChangedAtNeverGoesBackWithinARecord(t *testing.T) {
	backend, runID := historyFixture(t)
	const future = "2099-01-01T00:00:00.000Z"
	if _, err := backend.db.Exec(`INSERT INTO rnaseq_qc_history
		(run_id, record_key, field, old_value, old_is_null, new_value, new_is_null, source, changed_at)
		VALUES (?, 'R', 'note', NULL, 1, 'a', 0, 'scribo', ?)`, runID, future); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	appendOne(t, backend, "R", runID, now)
	appendOne(t, backend, "R2", runID, now)

	rows, err := backend.db.Query(`SELECT id, record_key, changed_at FROM rnaseq_qc_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var id, key, changedAt string
		if err := rows.Scan(&id, &key, &changedAt); err != nil {
			t.Fatal(err)
		}
		got = append(got, [3]string{id, key, changedAt})
	}
	want := [][3]string{
		{"1", "R", future},
		{"2", "R", future},
		{"3", "R2", "2026-11-06T12:00:00.000Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestProjectedRowWithoutAHistoryTable(t *testing.T) {
	backend := newQCBackend(t, qcDDL)
	ctx := context.Background()
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	row, err := tx.ProjectedRow(ctx, "rnaseq_qc", `[["replicate","1"],["sample","S1"]]`)

	if err != nil || len(row) != 0 {
		t.Errorf("ProjectedRow = %v, %v, want an empty row and no error", row, err)
	}
	var tables int
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := backend.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'rnaseq_qc_history'`).Scan(&tables); err != nil || tables != 0 {
		t.Errorf("history tables = %d, err %v, want 0", tables, err)
	}
}
