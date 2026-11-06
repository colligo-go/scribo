package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/colligo-go/scribo/internal/contract"
	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

var _ db.Backend = (*Backend)(nil)

type Backend struct {
	db          *sql.DB
	fs          FSInfo
	journalMode string
	warnings    []exitcode.Warning
	detect      func(string) (FSInfo, error)
}

func New() *Backend {
	return &Backend{detect: detectFS}
}

func (b *Backend) Open(ctx context.Context, path string, opts db.OpenOptions) error {
	if b.db != nil {
		return exitcode.New(exitcode.System, exitcode.CodeInternal, "backend already open", "")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return classifyFileError(err, path)
	}
	if _, err := os.Stat(absPath); err != nil {
		return classifyFileError(err, absPath)
	}
	info, err := b.detect(filepath.Dir(absPath))
	if err != nil {
		return classifyFileError(err, absPath)
	}

	b.fs = info
	b.journalMode = "wal"
	b.warnings = nil
	if info.Network {
		b.journalMode = "delete"
		b.warnings = append(b.warnings, exitcode.Warning{
			Code:    exitcode.CodeNetworkFilesystem,
			Message: fmt.Sprintf("database on %s: journal mode DELETE instead of WAL", info.Name),
			Context: "WAL on a network filesystem corrupts in silence; concurrent writers should spool and use upsert --batch (SPEC §13)",
		})
	}

	busyTimeout := opts.BusyTimeout
	if busyTimeout == 0 {
		busyTimeout = defaultBusyTimeoutMS
	}

	handle, err := sql.Open("sqlite", buildDSN(absPath, b.journalMode, busyTimeout))
	if err != nil {
		return classifyFileError(err, absPath)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0)

	if err := handle.PingContext(ctx); err != nil {
		handle.Close()
		translated := translate(err)
		var domain *exitcode.Error
		if errors.As(translated, &domain) {
			return domain
		}
		return classifyFileError(translated, absPath)
	}
	b.db = handle

	if err := b.checkTable(ctx, "tool_runs", contract.ToolRunsDDL); err != nil {
		b.Close()
		return translate(err)
	}
	return nil
}

func (b *Backend) Close() error {
	if b.db == nil {
		return nil
	}
	err := b.db.Close()
	b.db = nil
	return translate(err)
}

func (b *Backend) JournalMode() string {
	return b.journalMode
}

func (b *Backend) Warnings() []exitcode.Warning {
	return b.warnings
}

func (b *Backend) TableExists(ctx context.Context, table string) (bool, error) {
	var found int
	err := b.db.QueryRowContext(ctx,
		"SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, translate(err)
	}
	return true, nil
}

func (b *Backend) inTx(ctx context.Context, work func(*sql.Tx) error) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
