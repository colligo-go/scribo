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
	"github.com/colligo-go/scribo/internal/validate"
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

func Upsert(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	if err := backend.Open(ctx, req.DB, db.OpenOptions{BusyTimeout: req.BusyTimeout}); err != nil {
		return err
	}
	defer backend.Close()
	for _, warning := range backend.Warnings() {
		warn(warning)
	}

	byName, err := validate.Columns(ctx, backend, req.Table, sortedNames(req.Key, req.Values))
	if err != nil {
		return err
	}
	canonicalKey, err := validate.CanonicalKey(byName, req.Key)
	if err != nil {
		return err
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
	writeErr := write(ctx, backend, req, byName, canonicalKey, recordKey, runID, warn)
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

func write(ctx context.Context, backend db.Backend, req Request, columns map[string]db.Column, key db.Key, recordKey string, runID int64, warn func(exitcode.Warning)) error {
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := time.Now()

	reconciled, deleted, err := reconcile(ctx, tx, req.Table, columns, key, recordKey, runID, at)
	if err != nil {
		return err
	}

	changes, err := tx.Upsert(ctx, req.Table, key, req.Values, req.Mode)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		if err := tx.AppendHistory(ctx, req.Table, recordKey, changes, runID, at, db.SourceScribo); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(reconciled) > 0 {
		warn(reconciledWarning(req.Table, recordKey, reconciled, deleted))
	}
	return nil
}

func reconcile(ctx context.Context, tx db.Tx, table string, columns map[string]db.Column, key db.Key, recordKey string, runID int64, at time.Time) ([]db.FieldChange, bool, error) {
	projected, err := tx.ProjectedRow(ctx, table, recordKey)
	if err != nil {
		return nil, false, err
	}
	for field := range projected {
		if _, ok := columns[field]; !ok {
			delete(projected, field)
		}
	}
	if len(projected) == 0 {
		return nil, false, nil
	}
	fields := make([]string, 0, len(projected))
	for field := range projected {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	current, found, err := tx.SelectRow(ctx, table, key, fields)
	if err != nil {
		return nil, false, err
	}
	changes := history.Reconcile(projected, current, found)
	if len(changes) == 0 {
		return nil, false, nil
	}
	if err := tx.AppendHistory(ctx, table, recordKey, changes, runID, at, db.SourceReconciled); err != nil {
		return nil, false, err
	}
	return changes, !found, nil
}

func reconciledWarning(table, recordKey string, changes []db.FieldChange, deleted bool) exitcode.Warning {
	message := fmt.Sprintf("%s: record deleted outside Scribo; history reconciled", table)
	if !deleted {
		fields := make([]string, len(changes))
		for i, change := range changes {
			fields[i] = change.Field
		}
		noun := "fields"
		if len(fields) == 1 {
			noun = "field"
		}
		message = fmt.Sprintf("%s: %d %s changed outside Scribo: %s; history reconciled",
			table, len(fields), noun, strings.Join(fields, ", "))
	}
	return exitcode.Warning{
		Code:    exitcode.CodeHistoryReconciled,
		Message: message,
		Context: "record " + recordKey,
	}
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
