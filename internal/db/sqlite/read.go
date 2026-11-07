package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
)

func (b *Backend) HistoryOf(ctx context.Context, table, recordKey, field string) ([]db.HistoryRow, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	rows, err := b.historyOf(ctx, table, recordKey, field)
	return rows, translate(err)
}

func (b *Backend) historyOf(ctx context.Context, table, recordKey, field string) ([]db.HistoryRow, error) {
	exists, err := b.tableExists(ctx, b.db, historyTableName(table))
	if err != nil || !exists {
		return nil, err
	}

	statement := fmt.Sprintf(`SELECT id, run_id, field, old_value, old_is_null, new_value, new_is_null, source, changed_at
		FROM %s WHERE record_key = ?`, quoteIdent(historyTableName(table)))
	args := []any{recordKey}
	if field != "" {
		statement += " AND field = ?"
		args = append(args, field)
	}
	statement += " ORDER BY changed_at, id"

	rows, err := b.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []db.HistoryRow
	for rows.Next() {
		var row db.HistoryRow
		var oldValue, newValue sql.NullString
		var oldIsNull, newIsNull int
		if err := rows.Scan(&row.ID, &row.RunID, &row.Field, &oldValue, &oldIsNull, &newValue, &newIsNull, &row.Source, &row.ChangedAt); err != nil {
			return nil, err
		}
		row.Old = db.Value{Raw: oldValue.String, IsNull: oldIsNull == 1}
		row.New = db.Value{Raw: newValue.String, IsNull: newIsNull == 1}
		history = append(history, row)
	}
	return history, rows.Err()
}

func (b *Backend) RecordExists(ctx context.Context, table string, key db.Key) (bool, error) {
	if err := b.guard(); err != nil {
		return false, err
	}
	columns, err := readColumns(ctx, b.db, table)
	if err != nil {
		return false, translate(err)
	}
	where, args, err := keyPredicate(table, columnIndex(columns), key)
	if err != nil {
		return false, err
	}
	var found int
	err = b.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE %s LIMIT 1", quoteIdent(table), where), args...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, translate(err)
	}
	return true, nil
}

func (b *Backend) Runs(ctx context.Context, ids []int64) (map[int64]db.RunInfo, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	runs := make(map[int64]db.RunInfo, len(ids))
	if len(ids) == 0 {
		return runs, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := b.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, tool_version, ifnull(step, ''), exit_code FROM tool_runs WHERE id IN (%s)`, placeholders), args...)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var info db.RunInfo
		if err := rows.Scan(&id, &info.ToolVersion, &info.Step, &info.ExitCode); err != nil {
			return nil, translate(err)
		}
		runs[id] = info
	}
	return runs, translate(rows.Err())
}

func (b *Backend) LastRowOfRun(ctx context.Context, table string, runID int64) (db.HistoryRow, bool, error) {
	if err := b.guard(); err != nil {
		return db.HistoryRow{}, false, err
	}
	exists, err := b.tableExists(ctx, b.db, historyTableName(table))
	if err != nil || !exists {
		return db.HistoryRow{}, false, err
	}
	var row db.HistoryRow
	err = b.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id, run_id, changed_at FROM %s WHERE run_id = ? ORDER BY changed_at DESC, id DESC LIMIT 1`,
			quoteIdent(historyTableName(table))), runID).Scan(&row.ID, &row.RunID, &row.ChangedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return db.HistoryRow{}, false, nil
	}
	if err != nil {
		return db.HistoryRow{}, false, translate(err)
	}
	return row, true, nil
}
