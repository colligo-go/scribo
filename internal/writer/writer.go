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

type Record struct {
	Key    db.Key
	Values db.Values
	Name   string
}

type Batch struct {
	DB          string
	Table       string
	Records     []Record
	Mode        db.WriteMode
	BusyTimeout int
	ArgvShape   string
	ColumnsHint string
}

type Change struct {
	Source db.HistorySource
	db.FieldChange
}

type plan struct {
	columns map[string]db.Column
	records []recordPlan
}

type recordPlan struct {
	Record
	key       db.Key
	recordKey string
}

func Upsert(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) error {
	return UpsertMany(ctx, backend, req.batch(), warn)
}

func UpsertMany(ctx context.Context, backend db.Backend, batch Batch, warn func(exitcode.Warning)) error {
	return run.Interrupted(ctx, upsertMany(ctx, backend, batch, warn))
}

func (r Request) batch() Batch {
	return Batch{
		DB:          r.DB,
		Table:       r.Table,
		Records:     []Record{{Key: r.Key, Values: r.Values}},
		Mode:        r.Mode,
		BusyTimeout: r.BusyTimeout,
		ArgvShape:   r.ArgvShape,
	}
}

func DryRun(ctx context.Context, backend db.Backend, req Request, warn func(exitcode.Warning)) ([]Change, error) {
	changes, err := dryRun(ctx, backend, req, warn)
	return changes, run.Interrupted(ctx, err)
}

func open(ctx context.Context, backend db.Backend, path string, busyTimeout int, warn func(exitcode.Warning)) error {
	if err := backend.Open(ctx, path, db.OpenOptions{BusyTimeout: busyTimeout}); err != nil {
		return err
	}
	for _, warning := range backend.Warnings() {
		warn(warning)
	}
	return nil
}

func prepare(ctx context.Context, backend db.Backend, batch Batch) (plan, error) {
	names := fieldNames(batch.Records)
	var byName map[string]db.Column
	var err error
	if batch.ColumnsHint != "" {
		byName, err = validate.AllColumns(ctx, backend, batch.Table, names, batch.ColumnsHint)
	} else {
		byName, err = validate.Columns(ctx, backend, batch.Table, names)
	}
	if err != nil {
		return plan{}, err
	}
	uniqueKeys, err := backend.UniqueKeys(ctx, batch.Table)
	if err != nil {
		return plan{}, err
	}

	p := plan{columns: byName, records: make([]recordPlan, len(batch.Records))}
	for i, record := range batch.Records {
		if err := validate.Key(batch.Table, uniqueKeys, record.Key); err != nil {
			return plan{}, err
		}
		canonicalKey, err := validate.CanonicalKey(byName, record.Key)
		if err != nil {
			return plan{}, inRecord(record, "", err)
		}
		for _, name := range sortedNames(nil, record.Values) {
			if _, err := db.Coerce(byName[name], record.Values[name]); err != nil {
				return plan{}, inRecord(record, name, err)
			}
		}
		recordKey, err := history.RecordKey(canonicalKey)
		if err != nil {
			return plan{}, inRecord(record, "", err)
		}
		p.records[i] = recordPlan{Record: record, key: canonicalKey, recordKey: recordKey}
	}
	return p, nil
}

func upsertMany(ctx context.Context, backend db.Backend, batch Batch, warn func(exitcode.Warning)) error {
	if err := open(ctx, backend, batch.DB, batch.BusyTimeout, warn); err != nil {
		return err
	}
	defer backend.Close()
	p, err := prepare(ctx, backend, batch)
	if err != nil {
		return err
	}

	if err := backend.EnsureContractTables(ctx); err != nil {
		return err
	}
	if err := backend.EnsureHistoryTable(ctx, batch.Table); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	runID, err := backend.BeginRun(ctx, run.Meta(batch.ArgvShape, ""))
	if err != nil {
		return err
	}
	writeErr := run.Interrupted(ctx, write(ctx, backend, batch, p, runID, warn))
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
	if err := open(ctx, backend, req.DB, req.BusyTimeout, warn); err != nil {
		return nil, err
	}
	defer backend.Close()
	p, err := prepare(ctx, backend, req.batch())
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
	record := p.records[0]
	reconciled, _, err := reconciliation(ctx, tx, req.Table, p.columns, record)
	if err != nil {
		return nil, err
	}
	written, err := tx.Upsert(ctx, req.Table, record.key, req.Values, req.Mode)
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

type reconciledRecord struct {
	record  recordPlan
	changes []db.FieldChange
	deleted bool
}

func write(ctx context.Context, backend db.Backend, batch Batch, p plan, runID int64, warn func(exitcode.Warning)) error {
	tx, err := backend.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := time.Now()

	var found []reconciledRecord
	for _, record := range p.records {
		reconciled, deleted, err := writeRecord(ctx, tx, batch, p.columns, record, runID, at)
		if err != nil {
			return inRecord(record.Record, "", err)
		}
		if len(reconciled) > 0 {
			found = append(found, reconciledRecord{record: record, changes: reconciled, deleted: deleted})
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	switch {
	case len(found) == 0:
	case len(found) == 1 && len(batch.Records) == 1:
		warn(reconciledWarning(batch.Table, found[0].record.recordKey, found[0].changes, found[0].deleted))
	default:
		warn(reconciledRecordsWarning(batch.Table, found))
	}
	return nil
}

func writeRecord(ctx context.Context, tx db.Tx, batch Batch, columns map[string]db.Column, record recordPlan, runID int64, at time.Time) ([]db.FieldChange, bool, error) {
	reconciled, deleted, err := reconciliation(ctx, tx, batch.Table, columns, record)
	if err != nil {
		return nil, false, err
	}
	if len(reconciled) > 0 {
		if err := tx.AppendHistory(ctx, batch.Table, record.recordKey, reconciled, runID, at, db.SourceReconciled); err != nil {
			return nil, false, err
		}
	}

	changes, err := tx.Upsert(ctx, batch.Table, record.key, record.Values, batch.Mode)
	if err != nil {
		return nil, false, err
	}
	if len(changes) > 0 {
		if err := tx.AppendHistory(ctx, batch.Table, record.recordKey, changes, runID, at, db.SourceScribo); err != nil {
			return nil, false, err
		}
	}
	return reconciled, deleted, nil
}

func reconciliation(ctx context.Context, tx db.Tx, table string, columns map[string]db.Column, record recordPlan) ([]db.FieldChange, bool, error) {
	projected, err := tx.ProjectedRow(ctx, table, record.recordKey)
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

	current, found, err := tx.SelectRow(ctx, table, record.key, fields)
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

func reconciledRecordsWarning(table string, found []reconciledRecord) exitcode.Warning {
	names := make([]string, len(found))
	for i, record := range found {
		names[i] = record.record.Name
		if names[i] == "" {
			names[i] = record.record.recordKey
		}
	}
	noun := "records"
	if len(names) == 1 {
		noun = "record"
	}
	return exitcode.Warning{
		Code:    exitcode.CodeHistoryReconciled,
		Message: fmt.Sprintf("%s: %d %s reconciled: %s", table, len(names), noun, strings.Join(names, ", ")),
		Context: "changes made outside Scribo were found and recorded in the history",
	}
}

func inRecord(record Record, field string, err error) error {
	var domain *exitcode.Error
	if record.Name == "" || !errors.As(err, &domain) {
		return err
	}
	where := describeKey(record.Key)
	if field != "" {
		where += fmt.Sprintf(", metric %q", field)
	}
	return exitcode.New(domain.Status, domain.Code, where+": "+domain.Message, domain.Context)
}

func describeKey(key db.Key) string {
	names := sortedNames(key, nil)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + " " + key[name].Raw
	}
	return strings.Join(parts, ", ")
}

func fieldNames(records []Record) []string {
	seen := map[string]bool{}
	var names []string
	for _, record := range records {
		for _, name := range sortedNames(record.Key, record.Values) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
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
