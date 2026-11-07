package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
)

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (b *Backend) Columns(ctx context.Context, table string) ([]db.Column, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	columns, err := readColumns(ctx, b.db, table)
	return columns, translate(err)
}

func readColumns(ctx context.Context, q querier, table string) ([]db.Column, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT name, type, "notnull" FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []db.Column
	for rows.Next() {
		var column db.Column
		var notNull int
		if err := rows.Scan(&column.Name, &column.DeclaredType, &notNull); err != nil {
			return nil, err
		}
		column.Affinity = db.AffinityOf(column.DeclaredType)
		column.Nullable = notNull == 0
		column.Declared = column.DeclaredType != ""
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

type tableColumn struct {
	name         string
	declaredType string
	pk           int
}

func (b *Backend) UniqueKeys(ctx context.Context, table string) ([][]string, error) {
	if err := b.guard(); err != nil {
		return nil, err
	}
	keys, err := readUniqueKeys(ctx, b.db, table)
	return keys, translate(err)
}

func readUniqueKeys(ctx context.Context, q querier, table string) ([][]string, error) {
	columns, err := readTableColumns(ctx, q, table)
	if err != nil {
		return nil, err
	}
	names := make(map[int]string, len(columns))
	for cid, column := range columns {
		names[cid] = column.name
	}

	indexes, err := uniqueIndexNames(ctx, q, table)
	if err != nil {
		return nil, err
	}

	var keys [][]string
	for _, index := range indexes {
		key, plain, err := indexColumns(ctx, q, index, names)
		if err != nil {
			return nil, err
		}
		if plain && len(key) > 0 {
			keys = append(keys, key)
		}
	}
	if alias := rowidAlias(columns); alias != "" {
		keys = append(keys, []string{alias})
	}
	return sortedUnique(keys), nil
}

func readTableColumns(ctx context.Context, q querier, table string) ([]tableColumn, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, type, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []tableColumn
	for rows.Next() {
		var column tableColumn
		if err := rows.Scan(&column.name, &column.declaredType, &column.pk); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func uniqueIndexNames(ctx context.Context, q querier, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_index_list(?) WHERE "unique" = 1 AND partial = 0 ORDER BY seq`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func indexColumns(ctx context.Context, q querier, index string, names map[int]string) ([]string, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT cid FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var key []string
	for rows.Next() {
		var cid int
		if err := rows.Scan(&cid); err != nil {
			return nil, false, err
		}
		name, ok := names[cid]
		if !ok {
			return nil, false, nil
		}
		key = append(key, name)
	}
	return key, true, rows.Err()
}

func rowidAlias(columns []tableColumn) string {
	alias := ""
	for _, column := range columns {
		if column.pk == 0 {
			continue
		}
		if alias != "" || column.pk != 1 {
			return ""
		}
		alias = column.name
		if !strings.EqualFold(column.declaredType, "INTEGER") {
			return ""
		}
	}
	return alias
}

func sortedUnique(keys [][]string) [][]string {
	seen := make(map[string]bool, len(keys))
	var unique [][]string
	for _, key := range keys {
		sort.Strings(key)
		id := strings.Join(key, "\x00")
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, key)
	}
	slices.SortFunc(unique, slices.Compare[[]string])
	return unique
}
