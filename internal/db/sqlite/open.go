package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"github.com/colligo-go/scribo/internal/exitcode"
	"modernc.org/sqlite"
)

const defaultBusyTimeoutMS = 5000

var verifiedPragmas = []string{"_busy_timeout", "_foreign_keys", "_journal_mode", "_synchronous"}

func init() {
	sqlite.RegisterConnectionHook(verifyPragmas)
}

func buildDSN(absPath string, journalMode string, busyTimeoutMS int) string {
	location := url.URL{Scheme: "file", Path: absPath}
	query := url.Values{}
	query.Set("mode", "rw")
	query.Set("_txlock", "immediate")
	query.Set("_busy_timeout", strconv.Itoa(busyTimeoutMS))
	query.Set("_foreign_keys", "1")
	query.Set("_journal_mode", journalMode)
	query.Set("_synchronous", "NORMAL")
	location.RawQuery = query.Encode()
	return location.String()
}

func verifyPragmas(conn sqlite.ExecQuerierContext, dsn string) error {
	location, err := url.Parse(dsn)
	if err != nil {
		return exitcode.New(exitcode.System, exitcode.CodePragmaIneffective,
			"cannot read the DSN back to verify the PRAGMAs", err.Error())
	}
	query := location.Query()
	for _, key := range verifiedPragmas {
		if !query.Has(key) {
			continue
		}
		name := strings.TrimPrefix(key, "_")
		want := expectedPragma(name, query.Get(key))
		got, err := readPragma(conn, name)
		if err != nil {
			return err
		}
		if got != want {
			return exitcode.New(exitcode.System, exitcode.CodePragmaIneffective,
				fmt.Sprintf("PRAGMA %s is %s, expected %s", name, got, want),
				pathOf(location))
		}
	}
	return nil
}

func expectedPragma(name, requested string) string {
	switch name {
	case "journal_mode":
		return strings.ToLower(requested)
	case "synchronous":
		switch strings.ToUpper(requested) {
		case "OFF":
			return "0"
		case "NORMAL":
			return "1"
		case "FULL":
			return "2"
		case "EXTRA":
			return "3"
		}
	}
	return requested
}

func readPragma(conn sqlite.ExecQuerierContext, name string) (string, error) {
	rows, err := conn.QueryContext(context.Background(), "PRAGMA "+name, nil)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	values := make([]driver.Value, len(rows.Columns()))
	if err := rows.Next(values); err != nil {
		return "", err
	}
	switch value := values[0].(type) {
	case []byte:
		return strings.ToLower(string(value)), nil
	case string:
		return strings.ToLower(value), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func pathOf(location *url.URL) string {
	if location.Path != "" {
		return location.Path
	}
	return location.Opaque
}

func classifyFileError(err error, path string) *exitcode.Error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return exitcode.New(exitcode.File, exitcode.CodeFileNotFound,
			"database file or its directory does not exist",
			path+": scribo opens an existing database and never creates one")
	case errors.Is(err, fs.ErrPermission):
		return exitcode.New(exitcode.System, exitcode.CodePermissionDenied,
			"permission denied on the database file or its directory",
			path)
	default:
		return exitcode.New(exitcode.System, exitcode.CodeIOError,
			err.Error(),
			path)
	}
}
