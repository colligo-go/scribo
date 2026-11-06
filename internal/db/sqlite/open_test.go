package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func localFS(string) (FSInfo, error) { return FSInfo{Name: "ext4"}, nil }

func networkFS(string) (FSInfo, error) { return FSInfo{Name: "nfs4", Network: true}, nil }

func newDatabaseFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "study.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openBackend(t *testing.T, path string, detect func(string) (FSInfo, error), opts db.OpenOptions) *Backend {
	t.Helper()
	backend := New()
	backend.detect = detect
	if err := backend.Open(context.Background(), path, opts); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { backend.Close() })
	return backend
}

func pragma(t *testing.T, backend *Backend, name string) string {
	t.Helper()
	var value string
	if err := backend.db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return value
}

func openError(t *testing.T, path string) *exitcode.Error {
	t.Helper()
	backend := New()
	backend.detect = localFS
	err := backend.Open(context.Background(), path, db.OpenOptions{})
	if err == nil {
		backend.Close()
		t.Fatal("Open succeeded, want an error")
	}
	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("error %v is not an *exitcode.Error", err)
	}
	return domain
}

func TestOpenLocalDatabase(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})

	if got := backend.JournalMode(); got != "wal" {
		t.Errorf("JournalMode() = %q, want wal", got)
	}
	for name, want := range map[string]string{
		"journal_mode": "wal",
		"foreign_keys": "1",
		"synchronous":  "1",
		"busy_timeout": "5000",
	} {
		if got := pragma(t, backend, name); got != want {
			t.Errorf("PRAGMA %s = %q, want %q", name, got, want)
		}
	}
	if warnings := backend.Warnings(); len(warnings) != 0 {
		t.Errorf("Warnings() = %v, want none", warnings)
	}
}

func TestOpenCustomBusyTimeout(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{BusyTimeout: 1234})

	if got := pragma(t, backend, "busy_timeout"); got != "1234" {
		t.Errorf("PRAGMA busy_timeout = %q, want 1234", got)
	}
}

func TestOpenNetworkFilesystemUsesDelete(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), networkFS, db.OpenOptions{})

	if got := backend.JournalMode(); got != "delete" {
		t.Errorf("JournalMode() = %q, want delete", got)
	}
	if got := pragma(t, backend, "journal_mode"); got != "delete" {
		t.Errorf("PRAGMA journal_mode = %q, want delete", got)
	}
	warnings := backend.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("Warnings() = %v, want exactly one", warnings)
	}
	if warnings[0].Code != "network_filesystem" || !strings.Contains(warnings[0].Message, "nfs4") {
		t.Errorf("warning = %+v, want code network_filesystem naming nfs4", warnings[0])
	}
}

func TestOpenWalDatabaseMovedToNetworkFilesystem(t *testing.T) {
	path := newDatabaseFile(t)

	local := openBackend(t, path, localFS, db.OpenOptions{})
	if _, err := local.db.Exec("CREATE TABLE t (x INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if got := pragma(t, local, "journal_mode"); got != "wal" {
		t.Fatalf("first open journal_mode = %q, want wal", got)
	}
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}

	remote := openBackend(t, path, networkFS, db.OpenOptions{})
	if got := pragma(t, remote, "journal_mode"); got != "delete" {
		t.Errorf("reopened journal_mode = %q, want delete", got)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind after Close: %v", suffix, err)
		}
	}
}

func TestOpenPathWithAwkwardCharacters(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir with space#hash?query%25pct")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "my study #1?x%20y.db"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	backend := openBackend(t, path, localFS, db.OpenOptions{})
	if _, err := backend.db.Exec("CREATE TABLE t (x INTEGER)"); err != nil {
		t.Fatal(err)
	}

	allowed := map[string]bool{name: true, name + "-wal": true, name + "-shm": true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Errorf("unexpected file %q in the database directory", entry.Name())
		}
	}
	roots, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Errorf("parent directory has %d entries, want 1", len(roots))
	}
}

func TestOpenMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.db")

	got := openError(t, path)

	if got.Status != exitcode.File || got.Code != "file_not_found" {
		t.Errorf("got status %d code %q, want %d file_not_found", got.Status, got.Code, exitcode.File)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open created the file: %v", err)
	}
}

func TestOpenMissingDirectory(t *testing.T) {
	got := openError(t, filepath.Join(t.TempDir(), "no", "such", "dir", "study.db"))

	if got.Status != exitcode.File || got.Code != "file_not_found" {
		t.Errorf("got status %d code %q, want %d file_not_found", got.Status, got.Code, exitcode.File)
	}
}

func TestOpenWithoutPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	path := newDatabaseFile(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	got := openError(t, path)

	if got.Status != exitcode.System || got.Code != "permission_denied" {
		t.Errorf("got status %d code %q, want %d permission_denied", got.Status, got.Code, exitcode.System)
	}
}

func TestReadBackCatchesIneffectivePragma(t *testing.T) {
	handle, err := sql.Open("sqlite", "file:mem?mode=memory&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	err = handle.PingContext(context.Background())

	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("Ping error = %v, want an *exitcode.Error", err)
	}
	if domain.Code != "pragma_ineffective" || domain.Status != exitcode.System {
		t.Errorf("got status %d code %q, want %d pragma_ineffective", domain.Status, domain.Code, exitcode.System)
	}
	if !strings.Contains(domain.Message, "memory") {
		t.Errorf("message %q does not name the actual mode", domain.Message)
	}
}

func TestTransactionsTakeTheWriteLockImmediately(t *testing.T) {
	path := newDatabaseFile(t)
	first := openBackend(t, path, localFS, db.OpenOptions{})
	second := openBackend(t, path, localFS, db.OpenOptions{BusyTimeout: 100})

	held, err := first.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()

	start := time.Now()
	blocked, err := second.db.BeginTx(context.Background(), nil)
	elapsed := time.Since(start)
	if err == nil {
		blocked.Rollback()
		t.Fatal("second BeginTx succeeded while the first held the write lock")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("error = %v, want database is locked", err)
	}
	if elapsed > time.Second {
		t.Errorf("BeginTx took %v to fail, want under 1s", elapsed)
	}
}

func TestTableExists(t *testing.T) {
	backend := openBackend(t, newDatabaseFile(t), localFS, db.OpenOptions{})
	if _, err := backend.db.Exec("CREATE TABLE results (x INTEGER)"); err != nil {
		t.Fatal(err)
	}

	for table, want := range map[string]bool{"results": true, "nope": false} {
		got, err := backend.TableExists(context.Background(), table)
		if err != nil {
			t.Fatalf("TableExists(%q): %v", table, err)
		}
		if got != want {
			t.Errorf("TableExists(%q) = %v, want %v", table, got, want)
		}
	}
}
