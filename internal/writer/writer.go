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

type Change struct {
	Source db.HistorySource
	db.FieldChange
}

type plan struct {
	columns   map[string]db.Column
	key       db.Key
	recordKey string
}

func Upsert(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	return run.Interrupted(ctx, upsert(ctx, backend, req, warn))
}

func DryRun(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) ([]Change, error) {
	changes, err := dryRun(ctx, backend, req, warn)
	return changes, run.Interrupted(ctx, err)
}

func open(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	if err := backend.Open(ctx, req.DB, db.OpenOptions{BusyTimeout: req.BusyTimeout}); err != nil {
		return err
	}
	for _, warning := range backend.Warnings() {
		warn(warning)
	}
	return nil
}

func prepare(ctx context.Context, backend db.Backend, req Request) (plan, error) {
	byName, err := validate.Columns(ctx, backend, req.Table, sortedNames(req.Key, req.Values))
	if err != nil {
		return plan{}, err
	}
	uniqueKeys, err := backend.UniqueKeys(ctx, req.Table)
	if err != nil {
		return plan{}, err
	}
	if err := validate.Key(req.Table, uniqueKeys, req.Key); err != nil {
		return plan{}, err
	}
	canonicalKey, err := validate.CanonicalKey(byName, req.Key)
	if err != nil {
		return plan{}, err
	}
	for name, value := range req.Values {
		if _, err := db.Coerce(byName[name], value); err != nil {
			return plan{}, err
		}
	}
	recordKey, err := history.RecordKey(canonicalKey)
	if err != nil {
		return plan{}, err
	}
	return plan{columns: byName, key: canonicalKey, recordKey: recordKey}, nil
}

func upsert(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	if err := open(ctx, backend, req, warn); err != nil {
		return err
	}
	defer backend.Close()
	p, err := prepare(ctx, backend, req)
	if err != nil {
		return err
	}

	if err := backend.EnsureContractTables(ctx); err != nil {
		return err
	}
	if err := backend.EnsureHistoryTable(ctx, req.Table); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	runID, err := backend.BeginRun(ctx, run.Meta(req.ArgvShape, ""))
	if err != nil {
		return err
	}
	writeErr := run.Interrupted(ctx, write(ctx, backend, req, p, runID, warn))
	endCtx, cancel := run.EndContext(ctx)
	defer cancel()
	if writeErr != nil {
		if endErr := backend.EndRun(endCtx, runID, statusOf(writeErr)); endErr != nil {
			warn(exitcode.Warning{
				Code:    exitcode.CodeInternal,
				Message: fmt.Sprintf("could not close run %d in tool_runs: %v", runID, endErr),
			})
		}
		return writeErr
	}
	return backend.EndRun(endCtx, runID, exitcode.OK)
}

func dryRun(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) ([]Change, error) {
	if err := open(ctx, backend, req, warn); err != nil {
		return nil, err
	}
	defer backend.Close()
	p, err := prepare(ctx, backend, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tx, err := backend.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	reconciled, _, err := reconciliation(ctx, tx, req.Table, p)
	if err != nil {
		return nil, err
	}
	written, err := tx.Upsert(ctx, req.Table, p.key, req.Values, req.Mode)
	if err != nil {
		return nil, err
	}
	if err := tx.Rollback(); err != nil {
		return nil, err
	}

	changes := make([]Change, 0, len(reconciled)+len(written))
	for _, change := range reconciled {
		changes = append(changes, Change{Source: db.SourceReconciled, FieldChange: change})
	}
	for _, change := range written {
		changes = append(changes, Change{Source: db.SourceScribo, FieldChange: change})
	}
	return changes, nil
}

func write(ctx context.Context, backend db.Backend, req Request, p plan, runID int64, warn func(exitcode.Warning)) error {
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := time.Now()

	reconciled, deleted, err := reconciliation(ctx, tx, req.Table, p)
	if err != nil {
		return err
	}
	if len(reconciled) > 0 {
		if err := tx.AppendHistory(ctx, req.Table, p.recordKey, reconciled, runID, at, db.SourceReconciled); err != nil {
			return err
		}
	}

	changes, err := tx.Upsert(ctx, req.Table, p.key, req.Values, req.Mode)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		if err := tx.AppendHistory(ctx, req.Table, p.recordKey, changes, runID, at, db.SourceScribo); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(reconciled) > 0 {
		warn(reconciledWarning(req.Table, p.recordKey, reconciled, deleted))
	}
	return nil
}

func reconciliation(ctx context.Context, tx db.Tx, table string, p plan) ([]db.FieldChange, bool, error) {
	projected, err := tx.ProjectedRow(ctx, table, p.recordKey)
	if err != nil {
		return nil, false, err
	}
	for field := range projected {
		if _, ok := p.columns[field]; !ok {
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

	current, found, err := tx.SelectRow(ctx, table, p.key, fields)
	if err != nil {
		return nil, false, err
	}
	changes := history.Reconcile(projected, current, found)
	return changes, !found && len(changes) > 0, nil
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
