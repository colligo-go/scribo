package cmd

import (
	"fmt"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func usageFor(command, format string, args ...any) *exitcode.Error {
	return exitcode.New(exitcode.Usage, exitcode.CodeUsage,
		fmt.Sprintf(format, args...), fmt.Sprintf("run 'scribo %s --help' for usage", command))
}

func splitAssignment(text string) (string, string, bool) {
	return strings.Cut(text, "=")
}

func requireGlobals(options *globalOptions, command string) error {
	if options.db == "" {
		return usageFor(command, "--db is required")
	}
	if options.table == "" {
		return usageFor(command, "--table is required")
	}
	if options.busyTimeout <= 0 {
		return usageFor(command, "--busy-timeout must be greater than zero")
	}
	return nil
}

func parseKeys(texts []string, command string) (db.Key, error) {
	key := db.Key{}
	for _, text := range texts {
		name, value, ok := splitAssignment(text)
		if !ok || name == "" {
			return nil, usageFor(command, "--key %q must be FIELD=VALUE", text)
		}
		if _, repeated := key[name]; repeated {
			return nil, usageFor(command, "key field %q is given twice", name)
		}
		key[name] = db.Value{Raw: value}
	}
	if len(key) == 0 {
		return nil, usageFor(command, "--key is required")
	}
	return key, nil
}
