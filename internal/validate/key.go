package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func Key(table string, uniqueKeys [][]string, key db.Key) error {
	given := make(map[string]string, len(key))
	for name := range key {
		given[strings.ToLower(name)] = name
	}

	var closest []string
	var closestMissing, closestExtra []string
	best := -1
	for _, unique := range uniqueKeys {
		missing, extra := difference(unique, given)
		if len(missing) == 0 && len(extra) == 0 {
			return nil
		}
		if distance := len(missing) + len(extra); best < 0 || distance < best {
			best, closest, closestMissing, closestExtra = distance, unique, missing, extra
		}
	}

	if len(uniqueKeys) == 0 {
		return exitcode.New(exitcode.Database, exitcode.CodeNoUniqueKey,
			fmt.Sprintf("%s has no unique key", table),
			"declare the key columns UNIQUE, for example UNIQUE(sample, replicate)")
	}

	var problems []string
	if len(closestMissing) > 0 {
		problems = append(problems, listOf(closestMissing, "is missing", "are missing"))
	}
	if len(closestExtra) > 0 {
		problems = append(problems, listOf(closestExtra, "is not part of it", "are not part of it"))
	}
	context := ""
	if len(uniqueKeys) > 1 {
		context = "unique keys: " + formatKeys(uniqueKeys)
	}
	return exitcode.New(exitcode.Database, exitcode.CodeKeyMismatch,
		fmt.Sprintf("the key of %s is (%s); %s", table, strings.Join(closest, ", "), strings.Join(problems, "; ")),
		context)
}

func difference(unique []string, given map[string]string) (missing, extra []string) {
	inUnique := make(map[string]bool, len(unique))
	for _, name := range unique {
		lower := strings.ToLower(name)
		inUnique[lower] = true
		if _, ok := given[lower]; !ok {
			missing = append(missing, name)
		}
	}
	for lower, name := range given {
		if !inUnique[lower] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return missing, extra
}

func listOf(names []string, one, many string) string {
	if len(names) == 1 {
		return names[0] + " " + one
	}
	return strings.Join(names, ", ") + " " + many
}

func formatKeys(keys [][]string) string {
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = "(" + strings.Join(key, ", ") + ")"
	}
	return strings.Join(parts, "; ")
}
