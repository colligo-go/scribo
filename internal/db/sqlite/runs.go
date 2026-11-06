package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/colligo-go/scribo/internal/contract"
	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func timestamp(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

var now = func() string {
	return timestamp(time.Now())
}

func nullable(text string) sql.NullString {
	return sql.NullString{String: text, Valid: text != ""}
}

func (b *Backend) BeginRun(ctx context.Context, meta db.RunMeta) (int64, error) {
	if err := b.guard(); err != nil {
		return 0, err
	}
	var runID int64
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`INSERT INTO tool_runs
				(contract_version, tool, tool_version, git_sha, go_version, host, step, config_hash, argv_shape, started_at)
			VALUES (?, 'scribo', ?, ?, ?, ?, ?, ?, ?, ?)`,
			contract.Version, meta.ToolVersion, meta.GitSHA, meta.GoVersion,
			nullable(meta.Host), nullable(meta.Step), nullable(meta.ConfigHash),
			meta.ArgvShape, now())
		if err != nil {
			return err
		}
		runID, err = result.LastInsertId()
		return err
	})
	return runID, translate(err)
}

func (b *Backend) EndRun(ctx context.Context, runID int64, exitCode int) error {
	if err := b.guard(); err != nil {
		return err
	}
	return translate(b.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE tool_runs SET ended_at = ?, exit_code = ?
			WHERE id = ? AND tool = 'scribo' AND ended_at IS NULL`,
			now(), exitCode, runID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return exitcode.New(exitcode.System, exitcode.CodeInternal,
				fmt.Sprintf("run %d is not an open Scribo run", runID), "")
		}
		return nil
	}))
}
