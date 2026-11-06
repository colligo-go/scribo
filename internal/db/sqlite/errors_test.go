package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func requireCode(t *testing.T, err error, status int, code string) *exitcode.Error {
	t.Helper()
	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("error = %v, want an *exitcode.Error", err)
	}
	if domain.Status != status || domain.Code != code {
		t.Errorf("got status %d code %q (%s), want %d %q", domain.Status, domain.Code, domain.Message, status, code)
	}
	return domain
}

func TestBusyDatabaseIsTyped(t *testing.T) {
	path := newDatabaseFile(t)
	first := New()
	first.detect = localFS
	if err := first.Open(context.Background(), path, db.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	holder, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	backend := openBackend(t, path, localFS, db.OpenOptions{BusyTimeout: 100})
	start := time.Now()
	err = backend.EnsureContractTables(context.Background())
	elapsed := time.Since(start)

	requireCode(t, err, exitcode.System, "db_busy")
	if elapsed > time.Second {
		t.Errorf("EnsureContractTables took %v to fail, want under 1s", elapsed)
	}
}

func TestConstraintFailureIsTyped(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})
	if _, err := backend.db.Exec("CREATE TABLE t (x TEXT NOT NULL, y INTEGER)"); err != nil {
		t.Fatal(err)
	}

	_, err := backend.db.Exec("INSERT INTO t (y) VALUES (1)")

	requireCode(t, translate(err), exitcode.Data, "constraint_failed")
}

func TestTranslatePassesOtherErrorsThrough(t *testing.T) {
	plain := errors.New("plain")
	if got := translate(plain); got != plain {
		t.Errorf("translate(plain) = %v", got)
	}
	if translate(nil) != nil {
		t.Error("translate(nil) is not nil")
	}
	typed := exitcode.New(exitcode.Data, exitcode.CodeInvalidValue, "x", "")
	if got := translate(typed); got != error(typed) {
		t.Errorf("translate(typed) = %v", got)
	}
}

func TestReadOnlyFileIsPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := newDatabaseFile(t)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}

	backend := New()
	backend.detect = localFS
	err := backend.Open(context.Background(), path, db.OpenOptions{})
	if err == nil {
		defer backend.Close()
		err = backend.EnsureContractTables(context.Background())
	}

	requireCode(t, err, exitcode.System, "permission_denied")
}

func TestVerifyPragmasRefusesUnreadableDSN(t *testing.T) {
	err := verifyPragmas(nil, "%zz")

	requireCode(t, err, exitcode.System, "pragma_ineffective")
}

func TestOpenTwiceIsRefused(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})

	err := backend.Open(context.Background(), newDatabaseFile(t), db.OpenOptions{})

	requireCode(t, err, exitcode.System, "internal")
}
