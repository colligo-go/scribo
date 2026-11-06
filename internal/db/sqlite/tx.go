package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

var _ db.Tx = (*Tx)(nil)

type Tx struct {
	owner *Backend
	tx    *sql.Tx
}

func (b *Backend) BeginTx(ctx context.Context) (db.Tx, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, translate(err)
	}
	b.txOpen = true
	return &Tx{owner: b, tx: tx}, nil
}

func (t *Tx) Commit() error {
	err := t.tx.Commit()
	t.owner.txOpen = false
	return translate(err)
}

func (t *Tx) Rollback() error {
	err := t.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	t.owner.txOpen = false
	return translate(err)
}

func (t *Tx) SelectRow(ctx context.Context, table string, key db.Key, fields []string) (db.Row, bool, error) {
	columns, err := readColumns(ctx, t.tx, table)
	if err != nil {
		return nil, false, translate(err)
	}
	row, found, err := selectRow(ctx, t.tx, columns, table, key, fields)
	return row, found, translate(err)
}

func columnIndex(columns []db.Column) map[string]db.Column {
	byName := make(map[string]db.Column, len(columns))
	for _, column := range columns {
		byName[column.Name] = column
	}
	return byName
}

func columnNotFound(table, name string) *exitcode.Error {
	return exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
		fmt.Sprintf("column %s does not exist in %s", name, table), "")
}

func sortedKeyNames(key db.Key) []string {
	names := make([]string, 0, len(key))
	for name := range key {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func keyPredicate(table string, byName map[string]db.Column, key db.Key) (string, []any, error) {
	if len(key) == 0 {
		return "", nil, exitcode.New(exitcode.System, exitcode.CodeInternal, "empty record key", "")
	}
	var terms []string
	var args []any
	for _, name := range sortedKeyNames(key) {
		column, ok := byName[name]
		if !ok {
			return "", nil, columnNotFound(table, name)
		}
		bound, err := db.Coerce(column, key[name])
		if err != nil {
			return "", nil, err
		}
		terms = append(terms, quoteIdent(name)+" IS ?")
		args = append(args, bound)
	}
	return strings.Join(terms, " AND "), args, nil
}

func selectRow(ctx context.Context, q querier, columns []db.Column, table string, key db.Key, fields []string) (db.Row, bool, error) {
	byName := columnIndex(columns)
	where, args, err := keyPredicate(table, byName, key)
	if err != nil {
		return nil, false, err
	}

	projection := "1"
	if len(fields) > 0 {
		parts := make([]string, len(fields))
		for i, field := range fields {
			if _, ok := byName[field]; !ok {
				return nil, false, columnNotFound(table, field)
			}
			parts[i] = "+" + quoteIdent(field)
		}
		projection = strings.Join(parts, ", ")
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT 2", projection, quoteIdent(table), where)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var found db.Row
	matches := 0
	for rows.Next() {
		matches++
		if matches > 1 {
			break
		}
		cells := make([]any, len(fields)+boolToInt(len(fields) == 0))
		pointers := make([]any, len(cells))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, false, err
		}
		found = make(db.Row, len(fields))
		for i, field := range fields {
			value, err := db.Render(cells[i])
			if err != nil {
				return nil, false, err
			}
			found[field] = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	switch matches {
	case 0:
		return nil, false, nil
	case 1:
		return found, true, nil
	default:
		return nil, false, exitcode.New(exitcode.Data, exitcode.CodeAmbiguousKey,
			fmt.Sprintf("more than one record of %s matches the key", table),
			"declare the key columns UNIQUE")
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (t *Tx) Upsert(ctx context.Context, table string, key db.Key, values db.Values, mode db.WriteMode) ([]db.FieldChange, error) {
	changes, err := t.upsert(ctx, table, key, values, mode)
	return changes, translate(err)
}

func (t *Tx) upsert(ctx context.Context, table string, key db.Key, values db.Values, mode db.WriteMode) ([]db.FieldChange, error) {
	columns, err := readColumns(ctx, t.tx, table)
	if err != nil {
		return nil, err
	}
	byName := columnIndex(columns)

	fields := make([]string, 0, len(values))
	for name := range values {
		if _, ok := byName[name]; !ok {
			return nil, columnNotFound(table, name)
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)

	before, found, err := selectRow(ctx, t.tx, columns, table, key, fields)
	if err != nil {
		return nil, err
	}
	if found && mode == db.ModeInsertOnly {
		return nil, exitcode.New(exitcode.Data, exitcode.CodeKeyAlreadyExists,
			fmt.Sprintf("a record of %s with this key already exists", table), "drop --insert-only to update it")
	}
	if !found && mode == db.ModeUpdateOnly {
		return nil, exitcode.New(exitcode.Data, exitcode.CodeKeyNotFound,
			fmt.Sprintf("no record of %s has this key", table), "drop --update-only to create it")
	}

	bound := make(map[string]any, len(fields))
	for _, name := range fields {
		value, err := db.Coerce(byName[name], values[name])
		if err != nil {
			return nil, err
		}
		bound[name] = value
	}

	if found {
		if err := t.update(ctx, table, byName, key, fields, bound); err != nil {
			return nil, err
		}
	} else {
		if err := t.insert(ctx, table, byName, key, fields, bound); err != nil {
			return nil, err
		}
	}

	after, ok, err := selectRow(ctx, t.tx, columns, table, key, fields)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, exitcode.New(exitcode.System, exitcode.CodeInternal,
			"the record is missing right after its write", "")
	}

	var changes []db.FieldChange
	for _, name := range fields {
		oldValue := db.Value{IsNull: true}
		if found {
			oldValue = before[name]
		}
		if oldValue.Equal(after[name]) {
			continue
		}
		changes = append(changes, db.FieldChange{Field: name, OldValue: oldValue, NewValue: after[name]})
	}
	return changes, nil
}

func (t *Tx) update(ctx context.Context, table string, byName map[string]db.Column, key db.Key, fields []string, bound map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	where, whereArgs, err := keyPredicate(table, byName, key)
	if err != nil {
		return err
	}
	assignments := make([]string, len(fields))
	args := make([]any, 0, len(fields)+len(whereArgs))
	for i, name := range fields {
		assignments[i] = quoteIdent(name) + " = ?"
		args = append(args, bound[name])
	}
	args = append(args, whereArgs...)

	result, err := t.tx.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(table), strings.Join(assignments, ", "), where), args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return exitcode.New(exitcode.System, exitcode.CodeInternal,
			fmt.Sprintf("an update of %s changed %d rows, expected 1", table, affected), "")
	}
	return nil
}

func (t *Tx) insert(ctx context.Context, table string, byName map[string]db.Column, key db.Key, fields []string, bound map[string]any) error {
	var names []string
	var args []any
	for _, name := range sortedKeyNames(key) {
		column, ok := byName[name]
		if !ok {
			return columnNotFound(table, name)
		}
		value, err := db.Coerce(column, key[name])
		if err != nil {
			return err
		}
		names = append(names, quoteIdent(name))
		args = append(args, value)
	}
	for _, name := range fields {
		names = append(names, quoteIdent(name))
		args = append(args, bound[name])
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")
	_, err := t.tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdent(table), strings.Join(names, ", "), placeholders), args...)
	return err
}

func (t *Tx) ProjectedRow(ctx context.Context, table string, recordKey string) (db.Row, error) {
	rows, err := t.tx.QueryContext(ctx, fmt.Sprintf(`SELECT field, new_value, new_is_null FROM (
		SELECT field, new_value, new_is_null,
		       ROW_NUMBER() OVER (PARTITION BY field ORDER BY changed_at DESC, id DESC) AS rn
		FROM %s WHERE record_key = ?
	) WHERE rn = 1`, quoteIdent(historyTableName(table))), recordKey)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	projected := db.Row{}
	for rows.Next() {
		var field string
		var value sql.NullString
		var isNull int
		if err := rows.Scan(&field, &value, &isNull); err != nil {
			return nil, translate(err)
		}
		projected[field] = db.Value{Raw: value.String, IsNull: isNull == 1}
	}
	return projected, translate(rows.Err())
}

func (t *Tx) AppendHistory(ctx context.Context, table string, recordKey string, changes []db.FieldChange, runID int64, at time.Time, source db.HistorySource) error {
	sourceName := "scribo"
	if source == db.SourceReconciled {
		sourceName = "reconciled"
	}
	if len(changes) == 0 {
		return nil
	}
	history := quoteIdent(historyTableName(table))
	changedAt := timestamp(at)
	var latest sql.NullString
	err := t.tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT max(changed_at) FROM %s WHERE record_key = ?", history), recordKey).Scan(&latest)
	if err != nil {
		return translate(err)
	}
	if latest.Valid && latest.String > changedAt {
		changedAt = latest.String
	}

	statement := fmt.Sprintf(
		`INSERT INTO %s (run_id, record_key, field, old_value, old_is_null, new_value, new_is_null, source, changed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, history)
	for _, change := range changes {
		_, err := t.tx.ExecContext(ctx, statement,
			runID, recordKey, change.Field,
			nullableValue(change.OldValue), boolToInt(change.OldValue.IsNull),
			nullableValue(change.NewValue), boolToInt(change.NewValue.IsNull),
			sourceName, changedAt)
		if err != nil {
			return translate(err)
		}
	}
	return nil
}

func nullableValue(v db.Value) sql.NullString {
	return sql.NullString{String: v.Raw, Valid: !v.IsNull}
}
