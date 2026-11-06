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

var now = func() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func nullable(text string) sql.NullString {
	return sql.NullString{String: text, Valid: text != ""}
}

func (b *Backend) BeginRun(ctx context.Context, meta db.RunMeta) (int64, error) {
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
	return runID, err
}

func (b *Backend) EndRun(ctx context.Context, runID int64, exitCode int) error {
	return b.inTx(ctx, func(tx *sql.Tx) error {
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
	})
}
