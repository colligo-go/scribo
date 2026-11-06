package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func historyTableName(table string) string {
	return table + "_history"
}

func historyDDL(table string) string {
	history := quoteIdent(historyTableName(table))
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    record_key  TEXT NOT NULL,
    field       TEXT NOT NULL,
    old_value   TEXT,
    old_is_null INTEGER NOT NULL,
    new_value   TEXT,
    new_is_null INTEGER NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('scribo','reconciled')),
    changed_at  TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS %[2]s ON %[1]s(record_key, changed_at, id);
CREATE INDEX IF NOT EXISTS %[3]s ON %[1]s(field, changed_at);
CREATE INDEX IF NOT EXISTS %[4]s ON %[1]s(run_id);
`, history,
		quoteIdent("idx_"+historyTableName(table)+"_record"),
		quoteIdent("idx_"+historyTableName(table)+"_field"),
		quoteIdent("idx_"+historyTableName(table)+"_run"))
}

func (b *Backend) EnsureHistoryTable(ctx context.Context, table string) error {
	if err := b.guard(); err != nil {
		return err
	}
	ddl := historyDDL(table)
	if err := b.checkTable(ctx, historyTableName(table), ddl, "SPEC §4"); err != nil {
		return translate(err)
	}
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, ddl)
		return err
	})
	if err != nil {
		return translate(err)
	}
	return translate(b.checkTable(ctx, historyTableName(table), ddl, "SPEC §4"))
}
