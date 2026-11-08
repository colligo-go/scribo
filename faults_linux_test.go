package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runWithFileLimit(t *testing.T, limit int, args ...string) result {
	t.Helper()
	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		t.Skip("prlimit (util-linux) is not installed")
	}
	command := exec.Command(prlimit, append([]string{"--fsize=" + strconv.Itoa(limit), binaryPath}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func copyDB(t *testing.T, source string) string {
	t.Helper()
	if _, err := os.Stat(source + "-wal"); err == nil {
		t.Fatalf("%s has a WAL file; copy it only after a clean close", source)
	}
	path := filepath.Join(t.TempDir(), "study.db")
	if err := os.WriteFile(path, readBytes(t, source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var fileLimits = []int{40 << 10, 64 << 10, 96 << 10, 160 << 10, 256 << 10, 512 << 10, 1 << 20}

type limitOutcome struct {
	limit    int
	failed   bool
	midWrite bool
}

func requireSomeMidWrite(t *testing.T, outcomes chan limitOutcome) {
	t.Helper()
	close(outcomes)
	var seen []string
	midWrite := false
	for outcome := range outcomes {
		seen = append(seen, strconv.Itoa(outcome.limit>>10)+"K:"+strconv.FormatBool(outcome.failed)+"/"+strconv.FormatBool(outcome.midWrite))
		midWrite = midWrite || outcome.midWrite
	}
	if !midWrite {
		t.Errorf("no limit failed after the run began (limit:failed/midWrite %v); the test never reached the write", seen)
	}
}

func TestFullDiskDuringAnIngestWritesAllOrNothing(t *testing.T) {
	t.Parallel()
	const samples, metrics = 100, 5
	first := syntheticReport(t, samples, metrics, 1)
	second := syntheticReport(t, samples, metrics, 2)
	base := syntheticDB(t, first)
	requireIngested(t, runScribo(t, ingestTool(base, first)...))
	outcomes := make(chan limitOutcome, len(fileLimits))
	t.Cleanup(func() { requireSomeMidWrite(t, outcomes) })

	for _, limit := range fileLimits {
		t.Run(strconv.Itoa(limit>>10)+"K", func(t *testing.T) {
			t.Parallel()
			path := copyDB(t, base)
			before := snapshot(t, path, "big", "big_history")
			runsBefore := count(t, path, "tool_runs")

			got := runWithFileLimit(t, limit, ingestTool(path, second)...)

			requireIntegrity(t, path)
			if got.ExitCode == 0 {
				outcomes <- limitOutcome{limit: limit}
				if history, want := count(t, path, "big_history"), strconv.Itoa(2*samples*metrics); history != want {
					t.Errorf("success with %s history rows, want %s", history, want)
				}
				return
			}
			if !strings.Contains(got.Stderr, "ERROR [io_error]") {
				t.Errorf("exit %d, stderr %q, want io_error", got.ExitCode, got.Stderr)
			}
			if after := snapshot(t, path, "big", "big_history"); after != before {
				t.Errorf("a failed ingest changed the data or the history")
			}
			outcomes <- limitOutcome{limit: limit, failed: true, midWrite: count(t, path, "tool_runs") != runsBefore}
			requireOutput(t, runScribo(t, "query", "--db", path, "--table", "big", "--fields", "sample"))
		})
	}
}

func TestFullDiskDuringAnUpsertWritesNothing(t *testing.T) {
	t.Parallel()
	base := seeded(t)
	limits := fileLimits[:4]
	outcomes := make(chan limitOutcome, len(limits))
	t.Cleanup(func() { requireSomeMidWrite(t, outcomes) })

	for _, limit := range limits {
		t.Run(strconv.Itoa(limit>>10)+"K", func(t *testing.T) {
			t.Parallel()
			path := copyDB(t, base)
			before := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history")
			runsBefore := count(t, path, "tool_runs")

			got := runWithFileLimit(t, limit, "upsert", "--db", path, "--table", "rnaseq_qc",
				"--key", "sample=S1", "--key", "replicate=1", "note="+strings.Repeat("x", 64<<10))

			requireIntegrity(t, path)
			if got.ExitCode == 0 {
				outcomes <- limitOutcome{limit: limit}
				if n := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE field = 'note'"); n != "2" {
					t.Errorf("success with %s note history rows, want 2", n)
				}
				return
			}
			if after := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history"); after != before {
				t.Errorf("a failed upsert changed the data or the history")
			}
			outcomes <- limitOutcome{limit: limit, failed: true, midWrite: count(t, path, "tool_runs") != runsBefore}
		})
	}
}

func TestFullDiskDuringAnExportLeavesNoFile(t *testing.T) {
	t.Parallel()
	report := syntheticReport(t, 1000, 5, 1)
	path := syntheticDB(t, report)
	requireIngested(t, runScribo(t, ingestTool(path, report)...))
	failedWrite := 0
	for _, limit := range []int{48 << 10} {
		dir := t.TempDir()
		fresh := filepath.Join(dir, "new.tsv")
		existing := filepath.Join(dir, "old.tsv")
		if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		for _, out := range []string{fresh, existing} {
			got := runWithFileLimit(t, limit, "export", "--db", path, "--table", "big", "--out", out, "--overwrite")
			if got.ExitCode == 0 {
				t.Fatalf("limit %d: export of %s succeeded", limit, out)
			}
			if strings.Contains(got.Stderr, "ERROR [io_error]: cannot write "+out) {
				failedWrite++
			}
		}
		requireOnlyEntries(t, dir, "old.tsv")
		if got := readFile(t, existing); got != "old" {
			t.Errorf("limit %d: existing file = %q, want it intact", limit, got)
		}
	}
	if failedWrite == 0 {
		t.Errorf("no export failed while writing the file; the test never reached the write")
	}
	requireIntegrity(t, path)
}
