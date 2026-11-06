package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/colligo-go/scribo/internal/contract"
	"github.com/colligo-go/scribo/internal/exitcode"
)

type columnInfo struct {
	cid     int
	name    string
	typ     string
	notNull int
	dflt    sql.NullString
	pk      int
}

type foreignKeyInfo struct {
	table    string
	from     string
	to       sql.NullString
	onUpdate string
	onDelete string
	match    string
}

type tableShape struct {
	columns     []columnInfo
	foreignKeys []foreignKeyInfo
	strict      int
}

func (b *Backend) EnsureContractTables(ctx context.Context) error {
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, contract.ToolRunsDDL)
		return err
	})
	if err != nil {
		return translate(err)
	}
	return translate(b.checkTable(ctx, "tool_runs", contract.ToolRunsDDL))
}

func (b *Backend) checkTable(ctx context.Context, table, ddl string) error {
	exists, err := b.TableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	reference, err := sql.Open("sqlite", "file:expected?mode=memory")
	if err != nil {
		return err
	}
	defer reference.Close()
	reference.SetMaxOpenConns(1)
	if _, err := reference.ExecContext(ctx, ddl); err != nil {
		return err
	}

	actual, err := describeTable(ctx, b.db, table)
	if err != nil {
		return err
	}
	expected, err := describeTable(ctx, reference, table)
	if err != nil {
		return err
	}
	if difference := firstDifference(actual, expected); difference != "" {
		return exitcode.New(exitcode.Database, exitcode.CodeContractMismatch,
			fmt.Sprintf("%s differs from CONTRACT.md: %s", table, difference),
			"the database was created by an incompatible tool or version")
	}
	return nil
}

func describeTable(ctx context.Context, handle *sql.DB, table string) (tableShape, error) {
	var shape tableShape

	columns, err := handle.QueryContext(ctx,
		`SELECT cid, name, type, "notnull", dflt_value, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return shape, err
	}
	defer columns.Close()
	for columns.Next() {
		var column columnInfo
		if err := columns.Scan(&column.cid, &column.name, &column.typ, &column.notNull, &column.dflt, &column.pk); err != nil {
			return shape, err
		}
		shape.columns = append(shape.columns, column)
	}
	if err := columns.Err(); err != nil {
		return shape, err
	}

	keys, err := handle.QueryContext(ctx,
		`SELECT "table", "from", "to", on_update, on_delete, "match" FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		return shape, err
	}
	defer keys.Close()
	for keys.Next() {
		var key foreignKeyInfo
		if err := keys.Scan(&key.table, &key.from, &key.to, &key.onUpdate, &key.onDelete, &key.match); err != nil {
			return shape, err
		}
		shape.foreignKeys = append(shape.foreignKeys, key)
	}
	if err := keys.Err(); err != nil {
		return shape, err
	}

	err = handle.QueryRowContext(ctx,
		`SELECT "strict" FROM pragma_table_list(?) WHERE name = ?`, table, table).Scan(&shape.strict)
	return shape, err
}

func firstDifference(actual, expected tableShape) string {
	actualByName := map[string]columnInfo{}
	for _, column := range actual.columns {
		actualByName[column.name] = column
	}
	expectedByName := map[string]columnInfo{}
	for _, column := range expected.columns {
		expectedByName[column.name] = column
	}

	for _, want := range expected.columns {
		if _, ok := actualByName[want.name]; !ok {
			return fmt.Sprintf("column %s is missing", want.name)
		}
	}
	for _, got := range actual.columns {
		if _, ok := expectedByName[got.name]; !ok {
			return fmt.Sprintf("unexpected column %s", got.name)
		}
	}
	for _, want := range expected.columns {
		got := actualByName[want.name]
		switch {
		case got.cid != want.cid:
			return fmt.Sprintf("column %s is at position %d, expected %d", want.name, got.cid, want.cid)
		case got.typ != want.typ:
			return fmt.Sprintf("column %s is %s, expected %s", want.name, got.typ, want.typ)
		case got.notNull != want.notNull:
			return fmt.Sprintf("column %s has NOT NULL %d, expected %d", want.name, got.notNull, want.notNull)
		case got.dflt != want.dflt:
			return fmt.Sprintf("column %s has a different default value", want.name)
		case got.pk != want.pk:
			return fmt.Sprintf("column %s has primary key position %d, expected %d", want.name, got.pk, want.pk)
		}
	}

	if len(actual.foreignKeys) != len(expected.foreignKeys) {
		return fmt.Sprintf("it has %d foreign keys, expected %d", len(actual.foreignKeys), len(expected.foreignKeys))
	}
	for i, want := range expected.foreignKeys {
		if actual.foreignKeys[i] != want {
			return fmt.Sprintf("foreign key on %s differs", want.from)
		}
	}

	if actual.strict != expected.strict {
		return "table is not STRICT"
	}
	return ""
}
