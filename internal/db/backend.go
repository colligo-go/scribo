package db

import (
	"context"
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

	// Columns lists the columns of a table in declaration order.
	Columns(ctx context.Context, table string) ([]Column, error)

	// EnsureContractTables creates the contract tables Scribo may create,
	// with the exact DDL of CONTRACT.md, and checks them against it.
	EnsureContractTables(ctx context.Context) error

	// EnsureHistoryTable creates {table}_history and its indexes, and checks
	// an existing one against the DDL of SPEC §4.
	EnsureHistoryTable(ctx context.Context, table string) error

	// BeginTx opens a BEGIN IMMEDIATE transaction. While it is open every
	// other Backend method returns an internal error: the pool holds one
	// connection and the call would wait for ever.
	BeginTx(ctx context.Context) (Tx, error)

	// BeginRun records the start of an execution in tool_runs and returns
	// its id. The row is committed at once, so it survives a failed write.
	BeginRun(ctx context.Context, meta RunMeta) (int64, error)

	// EndRun closes a run opened by BeginRun with its exit code. It refuses
	// a run that is not an open Scribo run.
	EndRun(ctx context.Context, runID int64, exitCode int) error
}
