package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
)

func (b *Backend) SelectRows(ctx context.Context, table string, where []db.Predicate, fields []string, limit int) (db.Rows, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	rows, err := b.selectRows(ctx, table, where, fields, limit)
	return rows, translate(err)
}

func (b *Backend) selectRows(ctx context.Context, table string, where []db.Predicate, fields []string, limit int) (db.Rows, error) {
	columns, err := readColumns(ctx, b.db, table)
	if err != nil {
		return nil, err
	}
	byName := columnIndex(columns)

	projection := make([]string, len(fields))
	for i, field := range fields {
		if _, ok := byName[field]; !ok {
			return nil, columnNotFound(table, field)
		}
		projection[i] = "+" + quoteIdent(field)
	}
	condition, args, err := filterCondition(table, byName, where)
	if err != nil {
		return nil, err
	}
	order, err := b.readOrder(ctx, table)
	if err != nil {
		return nil, err
	}

	statement := fmt.Sprintf("SELECT %s FROM %s", strings.Join(projection, ", "), quoteIdent(table))
	if condition != "" {
		statement += " WHERE " + condition
	}
	statement += " ORDER BY " + order
	if limit > 0 {
		statement += " LIMIT ?"
		args = append(args, limit)
	}

	result, err := b.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var rows db.Rows
	for result.Next() {
		cells := make([]any, len(fields))
		pointers := make([]any, len(fields))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := result.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make(db.Row, len(fields))
		for i, field := range fields {
			value, err := db.Render(cells[i])
			if err != nil {
				return nil, err
			}
			row[field] = value
		}
		rows = append(rows, row)
	}
	return rows, result.Err()
}

func filterCondition(table string, byName map[string]db.Column, where []db.Predicate) (string, []any, error) {
	var terms []string
	var args []any
	for _, predicate := range where {
		column, ok := byName[predicate.Field]
		if !ok {
			return "", nil, columnNotFound(table, predicate.Field)
		}
		name := quoteIdent(predicate.Field)
		switch {
		case predicate.Op == db.OpIsNull:
			terms = append(terms, name+" IS NULL")
			continue
		case predicate.Op == db.OpNotNull:
			terms = append(terms, name+" IS NOT NULL")
			continue
		case predicate.AsText:
			name = "CAST(" + name + " AS TEXT)"
			args = append(args, predicate.Value.Raw)
		default:
			bound, err := db.Coerce(column, predicate.Value)
			if err != nil {
				return "", nil, err
			}
			args = append(args, bound)
		}
		operator := predicate.Op.String()
		if predicate.Op == db.OpNe {
			operator = "IS NOT"
		}
		terms = append(terms, name+" "+operator+" ?")
	}
	return strings.Join(terms, " AND "), args, nil
}

func (b *Backend) readOrder(ctx context.Context, table string) (string, error) {
	keys, err := readUniqueKeys(ctx, b.db, table)
	if err != nil {
		return "", err
	}
	var withoutRowid int
	if err := b.db.QueryRowContext(ctx, `SELECT wr FROM pragma_table_list(?)`, table).Scan(&withoutRowid); err != nil {
		return "", err
	}
	var parts []string
	if len(keys) > 0 {
		for _, name := range keys[0] {
			parts = append(parts, quoteIdent(name))
		}
	}
	if withoutRowid == 0 {
		parts = append(parts, "rowid")
	}
	return strings.Join(parts, ", "), nil
}
