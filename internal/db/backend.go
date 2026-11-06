package db

import (
	"context"

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

	// EnsureContractTables creates the contract tables Scribo may create,
	// with the exact DDL of CONTRACT.md, and checks them against it.
	EnsureContractTables(ctx context.Context) error

	// BeginRun records the start of an execution in tool_runs and returns
	// its id. The row is committed at once, so it survives a failed write.
	BeginRun(ctx context.Context, meta RunMeta) (int64, error)

	// EndRun closes a run opened by BeginRun with its exit code. It refuses
	// a run that is not an open Scribo run.
	EndRun(ctx context.Context, runID int64, exitCode int) error
}
