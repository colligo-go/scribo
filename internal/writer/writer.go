package writer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/history"
	"github.com/colligo-go/scribo/internal/run"
)

type Request struct {
	DB          string
	Table       string
	Key         db.Key
	Values      db.Values
	Mode        db.WriteMode
	BusyTimeout int
	ArgvShape   string
}

var reservedTables = map[string]bool{
	"tool_runs":     true,
	"result_inputs": true,
	"input_signals": true,
	"samples":       true,
	"files":         true,
	"facts":         true,
	"study_meta":    true,
}

func isReserved(table string) bool {
	lower := strings.ToLower(table)
	return reservedTables[lower] ||
		strings.HasSuffix(lower, "_history") ||
		strings.HasPrefix(lower, "sqlite_")
}

func Upsert(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	if err := backend.Open(ctx, req.DB, db.OpenOptions{BusyTimeout: req.BusyTimeout}); err != nil {
		return err
	}
	defer backend.Close()
	for _, warning := range backend.Warnings() {
		warn(warning)
	}

	if isReserved(req.Table) {
		return exitcode.New(exitcode.Database, exitcode.CodeReservedTable,
			fmt.Sprintf("%s is a reserved table", req.Table),
			"Scribo and Colligo own this table; name a data table")
	}
	exists, err := backend.TableExists(ctx, req.Table)
	if err != nil {
		return err
	}
	if !exists {
		return exitcode.New(exitcode.Database, exitcode.CodeTableNotFound,
			fmt.Sprintf("table %s does not exist", req.Table),
			"Scribo never creates a data table; create it first, for example from 'scribo schema'")
	}

	columns, err := backend.Columns(ctx, req.Table)
	if err != nil {
		return err
	}
	byName := make(map[string]db.Column, len(columns))
	names := make([]string, len(columns))
	for i, column := range columns {
		byName[column.Name] = column
		names[i] = column.Name
	}
	for _, name := range sortedNames(req.Key, req.Values) {
		if _, ok := byName[name]; !ok {
			return exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
				fmt.Sprintf("column %s does not exist in %s", name, req.Table),
				suggestion(name, names))
		}
	}

	canonicalKey := make(db.Key, len(req.Key))
	for name, value := range req.Key {
		canonical, err := db.Canonical(byName[name], value)
		if err != nil {
			return err
		}
		canonicalKey[name] = canonical
	}
	for name, value := range req.Values {
		if _, err := db.Coerce(byName[name], value); err != nil {
			return err
		}
	}
	recordKey, err := history.RecordKey(canonicalKey)
	if err != nil {
		return err
	}

	if err := backend.EnsureContractTables(ctx); err != nil {
		return err
	}
	if err := backend.EnsureHistoryTable(ctx, req.Table); err != nil {
		return err
	}

	runID, err := backend.BeginRun(ctx, run.Meta(req.ArgvShape, ""))
	if err != nil {
		return err
	}
	writeErr := write(ctx, backend, req, canonicalKey, recordKey, runID)
	if writeErr != nil {
		if endErr := backend.EndRun(ctx, runID, statusOf(writeErr)); endErr != nil {
			warn(exitcode.Warning{
				Code:    exitcode.CodeInternal,
				Message: fmt.Sprintf("could not close run %d in tool_runs: %v", runID, endErr),
			})
		}
		return writeErr
	}
	return backend.EndRun(ctx, runID, exitcode.OK)
}

func write(ctx context.Context, backend db.Backend, req Request, key db.Key, recordKey string, runID int64) error {
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	changes, err := tx.Upsert(ctx, req.Table, key, req.Values, req.Mode)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		if err := tx.AppendHistory(ctx, req.Table, recordKey, changes, runID, time.Now(), db.SourceScribo); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func statusOf(err error) int {
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		return domain.Status
	}
	return exitcode.System
}

func sortedNames(key db.Key, values db.Values) []string {
	names := make([]string, 0, len(key)+len(values))
	for name := range key {
		names = append(names, name)
	}
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
