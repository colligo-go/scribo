package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/colligo-go/scribo/internal/exitcode"
)

// OpenOptions tunes how a Backend opens a database.
type OpenOptions struct {
	// BusyTimeout is how long, in milliseconds, a connection waits for a
	// lock held by another process before giving up. Zero means 5000.
	BusyTimeout int
}

// RunMeta describes one execution of the tool, recorded in tool_runs. It
// holds the shape of the command, never an argument value.
type RunMeta struct {
	ToolVersion string
	GitSHA      string
	GoVersion   string
	ArgvShape   string
	Step        string
	Host        string
	ConfigHash  string
}

// HistoryRow is one row of {table}_history, with the old and new values
// back in Value form so that NULL and empty text stay apart.
type HistoryRow struct {
	ID        int64
	RunID     int64
	Field     string
	Source    string
	ChangedAt string
	Old       Value
	New       Value
}

// RunInfo is what log --with-runs shows of the run that wrote a history
// row. Step is empty when the run had none; ExitCode is not valid while the
// run is still open or was interrupted before it closed.
type RunInfo struct {
	ToolVersion string
	Step        string
	ExitCode    sql.NullInt64
}

// WriteMode says what an upsert may do when the record does or does not exist.
type WriteMode int

const (
	// ModeUpsert inserts a new record or updates an existing one.
	ModeUpsert WriteMode = iota
	// ModeInsertOnly refuses to touch an existing record.
	ModeInsertOnly
	// ModeUpdateOnly refuses to create a missing record.
	ModeUpdateOnly
)

// HistorySource says who wrote a history row: Scribo itself, or the
// reconciliation that found a change made behind its back.
type HistorySource int

const (
	SourceScribo HistorySource = iota
	SourceReconciled
)

// FieldChange is one field of one record that a write really changed. Both
// values are the stored form, read back after the write. A new record has a
// NULL OldValue for every field.
type FieldChange struct {
	Field    string
	OldValue Value
	NewValue Value
}

// Tx is one BEGIN IMMEDIATE transaction. It does not embed Backend: SQLite
// has no nested transactions, and the narrow interface makes a call to the
// Backend from inside a Tx impossible to write. Reads of the current state
// exist only here, so that no one can read the state they are about to write
// outside the transaction that writes it.
type Tx interface {
	// SelectRow reads the fields of the record the key names. The boolean
	// is false when there is no such record. Two or more matches are an
	// ambiguous_key error, never an arbitrary pick.
	SelectRow(ctx context.Context, table string, key Key, fields []string) (Row, bool, error)

	// ProjectedRow is the state the history asserts for a record: for each
	// field with history, the new value of its last row in (changed_at, id)
	// order. A record with no history, or a table with no {table}_history
	// yet, gives an empty Row and creates nothing.
	ProjectedRow(ctx context.Context, table string, recordKey string) (Row, error)

	// Upsert writes values to the record the key names, as mode allows, and
	// returns the fields whose stored value changed, ordered by field name.
	// Key fields never appear in the result.
	Upsert(ctx context.Context, table string, key Key, values Values, mode WriteMode) ([]FieldChange, error)

	// AppendHistory adds one history row per change under runID.
	AppendHistory(ctx context.Context, table string, recordKey string, changes []FieldChange, runID int64, at time.Time, source HistorySource) error

	Commit() error

	// Rollback undoes the transaction. It returns nil after a Commit, so
	// that "defer tx.Rollback()" is safe.
	Rollback() error
}

// Backend is the only door to persistence. Handlers never see SQL; they hold
// a Backend and call these methods. The interface grows one slice at a time
// toward the shape in docs/SPEC.md §2, and holds only what is implemented.
type Backend interface {
	// Open opens the database file at path, which must already exist. It
	// resolves the journal mode from the filesystem, applies the contract
	// PRAGMAs to every connection and reads them back.
	Open(ctx context.Context, path string, opts OpenOptions) error

	// Close releases the database. It is safe to call on a Backend that
	// never opened.
	Close() error

	// JournalMode reports the journal mode the database was opened with,
	// in lower case: "wal" or "delete".
	JournalMode() string

	// Warnings returns what Open found worth telling the user. A Backend
	// never writes to stderr itself; the command layer prints these.
	Warnings() []exitcode.Warning

	TableExists(ctx context.Context, table string) (bool, error)

	// Tables lists the names of every table of the database, in alphabetical
	// order, without SQLite's own sqlite_ tables. Telling a data table from
	// one that Scribo or Colligo owns is the caller's job.
	Tables(ctx context.Context) ([]string, error)

	// Columns lists the columns of a table in declaration order.
	Columns(ctx context.Context, table string) ([]Column, error)

	// EnsureContractTables creates the contract tables Scribo may create,
	// with the exact DDL of CONTRACT.md, and checks them against it.
	EnsureContractTables(ctx context.Context) error

	// EnsureHistoryTable creates {table}_history and its indexes, and checks
	// an existing one against the DDL of SPEC §4.
	EnsureHistoryTable(ctx context.Context, table string) error

	// UniqueKeys lists the column sets that identify one row of the table:
	// each UNIQUE constraint, each non-partial UNIQUE index over plain
	// columns, and the PRIMARY KEY (an INTEGER PRIMARY KEY included). Column
	// names carry the spelling of the table, each set and the list are
	// sorted, and no set repeats. A table with no unique key gives an empty
	// list.
	UniqueKeys(ctx context.Context, table string) ([][]string, error)

	// SelectRows reads the given fields of every record that matches all
	// the predicates, at most limit records when limit is greater than
	// zero. Records come ordered by the columns of the table's first unique
	// key, or by rowid when it has none, so two reads of the same data give
	// the same order. Values are rendered like the history's. It reads
	// outside any transaction and never feeds the history.
	SelectRows(ctx context.Context, table string, where []Predicate, fields []string, limit int) (Rows, error)

	// BeginTx opens a BEGIN IMMEDIATE transaction. While it is open every
	// other Backend method returns an internal error: the pool holds one
	// connection and the call would wait for ever.
	BeginTx(ctx context.Context) (Tx, error)

	// HistoryOf returns the history rows of one record, or of one field of
	// it when field is not empty, in (changed_at, id) order. A table that has
	// no {table}_history yet gives no rows and no error, and nothing is
	// created.
	HistoryOf(ctx context.Context, table, recordKey, field string) ([]HistoryRow, error)

	// RecordExists reports whether a record with this key is in the table.
	// It reads outside any transaction, so its answer must never feed the
	// history; only Tx.SelectRow does that.
	RecordExists(ctx context.Context, table string, key Key) (bool, error)

	// Runs returns the tool_runs details of the given run ids. An id with no
	// row is absent from the map.
	Runs(ctx context.Context, ids []int64) (map[int64]RunInfo, error)

	// LastRowOfRun returns the last history row, in (changed_at, id) order,
	// that run runID wrote to {table}_history, whatever the record. The
	// boolean is false when the run wrote nothing there, or the table has no
	// history yet.
	LastRowOfRun(ctx context.Context, table string, runID int64) (HistoryRow, bool, error)

	// BeginRun records the start of an execution in tool_runs and returns
	// its id. The row is committed at once, so it survives a failed write.
	BeginRun(ctx context.Context, meta RunMeta) (int64, error)

	// EndRun closes a run opened by BeginRun with its exit code. It refuses
	// a run that is not an open Scribo run.
	EndRun(ctx context.Context, runID int64, exitCode int) error
}
