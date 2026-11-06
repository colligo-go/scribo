package sqlite

import (
	"context"
	"database/sql"

	"github.com/colligo-go/scribo/internal/db"
)

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (b *Backend) Columns(ctx context.Context, table string) ([]db.Column, error) {
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
