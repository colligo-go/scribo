package reader

import (
	"context"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/run"
	"github.com/colligo-go/scribo/internal/validate"
)

type QueryRequest struct {
	DB          string
	Table       string
	Fields      []string
	Filters     validate.Filters
	Limit       int
	BusyTimeout int
}

type QueryResult struct {
	Columns []string
	Rows    [][]db.Value
}

func Query(ctx context.Context, backend db.Backend, req QueryRequest, warn func(exitcode.Warning)) (QueryResult, error) {
	result, err := query(ctx, backend, req, warn)
	return result, run.Interrupted(ctx, err)
}

func query(ctx context.Context, backend db.Backend, req QueryRequest, warn func(exitcode.Warning)) (QueryResult, error) {
	filterFields, err := req.Filters.Fields()
	if err != nil {
		return QueryResult{}, err
	}
	if err := backend.Open(ctx, req.DB, db.OpenOptions{BusyTimeout: req.BusyTimeout}); err != nil {
		return QueryResult{}, err
	}
	defer backend.Close()
	for _, warning := range backend.Warnings() {
		warn(warning)
	}

	byName, err := validate.Columns(ctx, backend, req.Table, append(append([]string(nil), req.Fields...), filterFields...))
	if err != nil {
		return QueryResult{}, err
	}
	predicates, err := validate.Predicates(byName, req.Filters)
	if err != nil {
		return QueryResult{}, err
	}

	fields := req.Fields
	if len(fields) == 0 {
		columns, err := backend.Columns(ctx, req.Table)
		if err != nil {
			return QueryResult{}, err
		}
		for _, column := range columns {
			fields = append(fields, column.Name)
		}
	}

	rows, err := backend.SelectRows(ctx, req.Table, predicates, fields, req.Limit)
	if err != nil {
		return QueryResult{}, err
	}
	result := QueryResult{Columns: fields, Rows: make([][]db.Value, len(rows))}
	for i, row := range rows {
		cells := make([]db.Value, len(fields))
		for j, field := range fields {
			cells[j] = row[field]
		}
		result.Rows[i] = cells
	}
	return result, nil
}
