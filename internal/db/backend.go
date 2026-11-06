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
}
