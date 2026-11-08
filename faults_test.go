package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requireIntegrity(t *testing.T, path string) {
	t.Helper()
	if got := scalar(t, path, "PRAGMA integrity_check"); got != "ok" {
		t.Fatalf("integrity_check = %q, want ok", got)
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func syntheticReport(t *testing.T, samples, metrics, salt int) string {
	t.Helper()
	headers := map[string]any{}
	for m := 0; m < metrics; m++ {
		headers[fmt.Sprintf("m%d", m)] = map[string]any{"namespace": "Tool"}
	}
	data := map[string]any{}
	for s := 0; s < samples; s++ {
		values := map[string]any{}
		for m := 0; m < metrics; m++ {
			values[fmt.Sprintf("m%d", m)] = float64(salt*1000000+s*100+m) + 0.25
		}
		data[fmt.Sprintf("S%06d", s)] = values
	}
	content, err := json.Marshal(map[string]any{
		"report_general_stats_headers": map[string]any{"tool": headers},
		"report_general_stats_data":    map[string]any{"tool": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, fmt.Sprintf("report-%d-%d-%d.json", samples, metrics, salt), string(content))
}

func syntheticDB(t *testing.T, report string) string {
	t.Helper()
	ddl := runScribo(t, "schema", "--from-multiqc", report, "--table", "big", "--module", "tool")
	if ddl.ExitCode != 0 {
		t.Fatalf("schema: exit %d, stderr %q", ddl.ExitCode, ddl.Stderr)
	}
	return newDB(t, ddl.Stdout)
}

func ingestTool(path, report string) []string {
	return []string{"ingest", "multiqc", "--db", path, "--table", "big", "--module", "tool", report}
}

func requireIngested(t *testing.T, got result) {
	t.Helper()
	if got.ExitCode != 0 || got.Stdout != "" {
		t.Fatalf("ingest: exit %d, stdout %q, stderr %q", got.ExitCode, got.Stdout, got.Stderr)
	}
}

func TestCorruptDatabaseIsRefusedAndLeftAlone(t *testing.T) {
	t.Parallel()
	garbage := bytes.Repeat([]byte{0xA5, 0x3C, 0x00, 0xFF}, 25)
	tests := []struct {
		name    string
		damage  func(content []byte) []byte
		message string
	}{
		{"header overwritten", func(content []byte) []byte { return append(append([]byte{}, garbage...), content[len(garbage):]...) }, "file is not a database"},
		{"truncated in half", func(content []byte) []byte { return content[:len(content)/2] }, "database disk image is malformed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			report := syntheticReport(t, 400, 4, 1)
			path := syntheticDB(t, report)
			requireIngested(t, runScribo(t, ingestTool(path, report)...))
			damaged := tt.damage(readBytes(t, path))
			if err := os.WriteFile(path, damaged, 0o644); err != nil {
				t.Fatal(err)
			}

			for _, args := range [][]string{
				ingestTool(path, syntheticReport(t, 400, 4, 2)),
				{"upsert", "--db", path, "--table", "big", "--key", "sample=S000001", "m0=1"},
				{"query", "--db", path, "--table", "big"},
			} {
				got := runScribo(t, args...)
				requireFailure(t, got, 5, "io_error")
				if !strings.Contains(got.Stderr, tt.message) || !strings.Contains(got.Stderr, "→ "+path+"\n") {
					t.Errorf("%s: stderr = %q, want %q and the path", args[0], got.Stderr, tt.message)
				}
				if !bytes.Equal(readBytes(t, path), damaged) {
					t.Fatalf("%s changed the damaged file", args[0])
				}
			}
		})
	}
}

func TestReadOnlyDatabaseFileIsRefusedAndUnchanged(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only file")
	}
	path := seeded(t)
	before := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	got := upsertS1(t, path, "note=changed")

	requireFailure(t, got, 5, "permission_denied")
	requireIntegrity(t, path)
	if after := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs"); after != before {
		t.Errorf("state changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestKilledIngestLeavesAllOrNothing(t *testing.T) {
	t.Parallel()
	const samples, metrics = 400, 10
	report := syntheticReport(t, samples, metrics, 1)
	timed := syntheticDB(t, report)
	path := syntheticDB(t, report)
	start := time.Now()
	requireIngested(t, runScribo(t, ingestTool(timed, report)...))
	whole := time.Since(start)

	command := exec.Command(binaryPath, ingestTool(path, report)...)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(whole * 6 / 10)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	command.Wait()

	requireIntegrity(t, path)
	rows := count(t, path, "big")
	history := "0"
	if scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name = 'big_history'") == "1" {
		history = count(t, path, "big_history")
	}
	run := query(t, path, "SELECT ifnull(ended_at, 'NULL'), ifnull(exit_code, 'NULL') FROM tool_runs")
	switch {
	case rows == "0" && history == "0":
		if len(run) != 1 || run[0][0] != "NULL" || run[0][1] != "NULL" {
			t.Errorf("tool_runs = %v, want one run with ended_at and exit_code NULL", run)
		}
	case rows == strconv.Itoa(samples) && history == strconv.Itoa(samples*metrics):
		t.Fatalf("the ingest committed before the kill landed at %v of %v; tool_runs = %v", whole*6/10, whole, run)
	default:
		t.Fatalf("after the kill: %s rows and %s history rows, want 0 and 0 or %d and %d", rows, history, samples, samples*metrics)
	}

	requireSuccess(t, runScribo(t, "upsert", "--db", path, "--table", "big", "--key", "sample=S000001", "m0=1"))
	requireIntegrity(t, path)
}
