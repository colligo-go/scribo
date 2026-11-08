package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadingCommandsRefuseIncompleteArguments(t *testing.T) {
	t.Parallel()
	path := readingDB(t)
	tests := []struct {
		name    string
		args    []string
		mention string
	}{
		{"log without --db", []string{"log", "--table", "rnaseq_qc", "--key", "sample=S1"}, "--db"},
		{"log key without equals", []string{"log", "--db", path, "--table", "rnaseq_qc", "--key", "sample"}, "sample"},
		{"log unknown format", []string{"log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--format", "xml"}, `not "xml"`},
		{"query without --db", []string{"query", "--table", "rnaseq_qc"}, "--db"},
		{"export without --table", []string{"export", "--db", path}, "--table"},
		{"ingest without --db", []string{"ingest", "multiqc", "--table", "qc", multiqcFixture}, "--db"},
		{"ingest empty module", []string{"ingest", "multiqc", "--db", path, "--table", "qc", "--module", "", multiqcFixture}, "--module needs a module name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := runScribo(t, tt.args...)

			requireFailure(t, got, 1, "usage")
			if !strings.Contains(got.Stderr, tt.mention) {
				t.Errorf("stderr = %q, want it to mention %q", got.Stderr, tt.mention)
			}
		})
	}
}

func TestIngestOfADirectoryIsAnIOErrorAndWritesNothing(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))
	directory := t.TempDir()

	got := ingest(t, path, "qc", directory)

	requireFailure(t, got, 5, "io_error")
	if !strings.Contains(got.Stderr, "cannot read "+directory) {
		t.Errorf("stderr = %q", got.Stderr)
	}
	if n := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name = 'tool_runs'"); n != "0" {
		t.Error("an unreadable report began a run")
	}
}

func TestUnreadableInputsArePermissionDenied(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without read permission")
	}
	path := newDB(t, golden(t, generalGolden))
	report := writeTemp(t, "multiqc_data.json", `{"report_general_stats_data": {}}`)
	tsv := writeTemp(t, "results.tsv", "sample\tvalue\nS1\t1\n")
	for _, file := range []string{report, tsv} {
		if err := os.Chmod(file, 0o000); err != nil {
			t.Fatal(err)
		}
	}

	for name, args := range map[string][]string{
		"schema --from":         {"schema", "--from", tsv, "--table", "t", "--key", "sample"},
		"schema --from-multiqc": {"schema", "--from-multiqc", report, "--table", "t"},
		"ingest multiqc":        {"ingest", "multiqc", "--db", path, "--table", "qc", report},
	} {
		got := runScribo(t, args...)
		if got.ExitCode != 5 || !strings.Contains(got.Stderr, "ERROR [permission_denied]: cannot read ") {
			t.Errorf("%s: exit %d, stderr %q, want exit 5 permission_denied", name, got.ExitCode, got.Stderr)
		}
	}
}

func TestExportOutputPathsThatCannotBeWritten(t *testing.T) {
	t.Parallel()
	path := readingDB(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		out     string
		exit    int
		code    string
		mention string
	}{
		{"missing directory", filepath.Join(dir, "missing", "qc.tsv"), 4, "file_not_found", "the directory of"},
		{"below a regular file", filepath.Join(file, "qc.tsv"), 5, "io_error", "cannot write " + filepath.Join(file, "qc.tsv")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := runScribo(t, "export", "--db", path, "--table", "rnaseq_qc", "--out", tt.out)

			requireFailure(t, got, tt.exit, tt.code)
			if !strings.Contains(got.Stderr, tt.mention) {
				t.Errorf("stderr = %q, want it to mention %q", got.Stderr, tt.mention)
			}
		})
	}
	requireOnlyEntries(t, dir, "file.txt")
}

func TestEveryCommandRefusesAMissingDatabaseWithoutCreatingIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "study.db")
	commands := map[string][]string{
		"upsert": {"upsert", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1", "note=x"},
		"dry-run": {"upsert", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1", "note=x",
			"--dry-run"},
		"query":  {"query", "--db", path, "--table", "rnaseq_qc"},
		"export": {"export", "--db", path, "--table", "rnaseq_qc"},
		"log":    {"log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1"},
		"ingest": {"ingest", "multiqc", "--db", path, "--table", "qc", multiqcFixture},
	}
	for name, args := range commands {
		got := runScribo(t, args...)
		if got.ExitCode != 4 || !strings.Contains(got.Stderr, "ERROR [file_not_found]") || got.Stdout != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, want exit 4 file_not_found", name, got.ExitCode, got.Stdout, got.Stderr)
		}
	}
	requireOnlyEntries(t, dir)
}
