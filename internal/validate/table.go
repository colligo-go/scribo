package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

var reservedTables = map[string]bool{
	"tool_runs":     true,
	"result_inputs": true,
	"input_signals": true,
	"samples":       true,
	"files":         true,
	"facts":         true,
	"study_meta":    true,
}

func Reserved(table string) bool {
	lower := strings.ToLower(table)
	return reservedTables[lower] ||
		strings.HasSuffix(lower, "_history") ||
		strings.HasPrefix(lower, "sqlite_")
}

func Columns(ctx context.Context, backend db.Backend, table string, names []string) (map[string]db.Column, error) {
	byName, all, err := tableColumns(ctx, backend, table)
	if err != nil {
		return nil, err
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for _, name := range sorted {
		if _, ok := byName[name]; !ok {
			return nil, exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
				fmt.Sprintf("column %s does not exist in %s", name, table),
				Suggestion(name, all))
		}
	}
	return byName, nil
}

func AllColumns(ctx context.Context, backend db.Backend, table string, names []string, hint string) (map[string]db.Column, error) {
	byName, _, err := tableColumns(ctx, backend, table)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, name := range names {
		if _, ok := byName[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	switch len(missing) {
	case 0:
		return byName, nil
	case 1:
		return nil, exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
			fmt.Sprintf("column %s does not exist in %s", missing[0], table), hint)
	}
	return nil, exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
		fmt.Sprintf("%d columns do not exist in %s: %s", len(missing), table, strings.Join(missing, ", ")), hint)
}

func tableColumns(ctx context.Context, backend db.Backend, table string) (map[string]db.Column, []string, error) {
	if Reserved(table) {
		return nil, nil, exitcode.New(exitcode.Database, exitcode.CodeReservedTable,
			fmt.Sprintf("%s is a reserved table", table),
			"Scribo and Colligo own this table; name a data table")
	}
	exists, err := backend.TableExists(ctx, table)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, tableNotFound(ctx, backend, table)
	}

	columns, err := backend.Columns(ctx, table)
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]db.Column, len(columns))
	all := make([]string, len(columns))
	for i, column := range columns {
		byName[column.Name] = column
		all[i] = column.Name
	}
	return byName, all, nil
}

func CanonicalKey(columns map[string]db.Column, key db.Key) (db.Key, error) {
	canonical := make(db.Key, len(key))
	for name, value := range key {
		converted, err := db.Canonical(columns[name], value)
		if err != nil {
			return nil, err
		}
		canonical[name] = converted
	}
	return canonical, nil
}

func tableNotFound(ctx context.Context, backend db.Backend, table string) error {
	all, err := backend.Tables(ctx)
	if err != nil {
		return err
	}
	var data []string
	for _, name := range all {
		if !Reserved(name) {
			data = append(data, name)
		}
	}
	return exitcode.New(exitcode.Database, exitcode.CodeTableNotFound,
		fmt.Sprintf("table %s does not exist", table), TableSuggestion(table, data))
}
