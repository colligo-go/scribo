package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const historyHeader = "changed_at\trun_id\tsource\tfield\told_value\told_is_null\tnew_value\tnew_is_null"

func logS1(t *testing.T, path string, args ...string) result {
	t.Helper()
	base := []string{"log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S1", "--key", "replicate=1"}
	return runScribo(t, append(base, args...)...)
}

func requireData(t *testing.T, got result) []string {
	t.Helper()
	if got.ExitCode != 0 {
		t.Fatalf("exit %d, stderr %q", got.ExitCode, got.Stderr)
	}
	return strings.Split(strings.TrimSuffix(got.Stdout, "\n"), "\n")
}

func threeWrites(t *testing.T) string {
	t.Helper()
	path := seeded(t)
	time.Sleep(5 * time.Millisecond)
	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=13"))
	time.Sleep(5 * time.Millisecond)
	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=14"))
	return path
}

func changedAtOf(t *testing.T, path string, newValue string) time.Time {
	t.Helper()
	stamp := scalar(t, path, "SELECT changed_at FROM rnaseq_qc_history WHERE field = 'Mapped_Reads' AND new_value = ?", newValue)
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func between(t *testing.T, path, first, second string) time.Time {
	t.Helper()
	a, b := changedAtOf(t, path, first), changedAtOf(t, path, second)
	if !a.Before(b) {
		t.Fatalf("writes share a millisecond: %v and %v", a, b)
	}
	return a.Add(b.Sub(a) / 2)
}

func TestLogShowsTheWholeHistoryInOrder(t *testing.T) {
	path := threeWrites(t)

	got := logS1(t, path)

	lines := requireData(t, got)
	if got.Stderr != "" {
		t.Errorf("stderr = %q", got.Stderr)
	}
	if lines[0] != historyHeader {
		t.Fatalf("header = %q", lines[0])
	}
	if len(lines) != 6 {
		t.Fatalf("got %d data lines, want 5:\n%s", len(lines)-1, got.Stdout)
	}
	var reads []string
	for _, line := range lines[1:] {
		cells := strings.Split(line, "\t")
		if len(cells) != 8 || cells[2] != "scribo" {
			t.Fatalf("line %q", line)
		}
		if cells[3] == "Mapped_Reads" {
			reads = append(reads, cells[4]+"/"+cells[5]+">"+cells[6])
		}
	}
	if strings.Join(reads, " ") != "/1>12 12/0>13 13/0>14" {
		t.Errorf("Mapped_Reads lines = %v", reads)
	}
}

func TestLogFiltersByField(t *testing.T) {
	path := threeWrites(t)

	lines := requireData(t, logS1(t, path, "--field", "Mapped_Reads"))

	if len(lines) != 4 {
		t.Fatalf("lines = %v", lines)
	}
	for _, line := range lines[1:] {
		if strings.Split(line, "\t")[3] != "Mapped_Reads" {
			t.Errorf("line %q is not Mapped_Reads", line)
		}
	}
}

func TestLogEscapesTSV(t *testing.T) {
	path := newDB(t, qcTable)
	requireSuccess(t, upsertS1(t, path, "note=a\tb\nc\\d"))

	lines := requireData(t, logS1(t, path))

	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if cell := strings.Split(lines[1], "\t")[6]; cell != `a\tb\nc\\d` {
		t.Errorf("cell = %q", cell)
	}
}

func TestLogJSONKeepsNullAndTextApart(t *testing.T) {
	path := seeded(t)
	requireSuccess(t, upsertS1(t, path, "note=null"))
	requireSuccess(t, upsertS1(t, path, "--null", "note"))
	requireSuccess(t, upsertS1(t, path, "note="))

	lines := requireData(t, logS1(t, path, "--field", "note", "--format", "json"))

	var news []string
	for _, line := range lines {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		switch value := object["new"].(type) {
		case nil:
			news = append(news, "NULL")
		case string:
			news = append(news, `"`+value+`"`)
		}
	}
	if strings.Join(news, " ") != `"ok" "null" NULL ""` {
		t.Errorf("new values = %v", news)
	}
}

func TestLogAtShowsThePastState(t *testing.T) {
	path := threeWrites(t)
	moment := between(t, path, "12", "13")

	utc := requireData(t, logS1(t, path, "--at", moment.UTC().Format(time.RFC3339Nano)))
	offset := requireData(t, logS1(t, path, "--at", moment.In(time.FixedZone("", 2*3600)).Format(time.RFC3339Nano)))

	want := "field\tvalue\tis_null|Alignment_Rate\t94.3\t0|Mapped_Reads\t12\t0|note\tok\t0"
	if got := strings.Join(utc, "|"); got != want {
		t.Errorf("UTC output = %q, want %q", got, want)
	}
	if strings.Join(offset, "|") != strings.Join(utc, "|") {
		t.Errorf("+02:00 output = %q differs from UTC %q", offset, utc)
	}
}

func TestLogAtBeforeEverything(t *testing.T) {
	path := seeded(t)

	lines := requireData(t, logS1(t, path, "--at", "2000-01-01T00:00:00Z"))

	if strings.Join(lines, "|") != "field\tvalue\tis_null" {
		t.Errorf("output = %q", lines)
	}
}

func TestLogAtWarnsAcrossAReconciliation(t *testing.T) {
	path := seeded(t)
	time.Sleep(5 * time.Millisecond)
	mustExec(t, path, "UPDATE rnaseq_qc SET note = 'edited'")
	requireReconcileWarning(t, upsertS1(t, path, "Mapped_Reads=13"))
	moment := between(t, path, "12", "13")

	before := logS1(t, path, "--at", moment.Format(time.RFC3339Nano))
	after := logS1(t, path, "--at", time.Now().Add(time.Hour).Format(time.RFC3339Nano))

	requireData(t, before)
	if !strings.Contains(before.Stderr, "WARNING [history_approximate]") || !strings.Contains(before.Stderr, "note") {
		t.Errorf("before the detection: stderr = %q", before.Stderr)
	}
	requireData(t, after)
	if after.Stderr != "" {
		t.Errorf("after the detection: stderr = %q", after.Stderr)
	}
}

func TestLogRefusesAmbiguousMoments(t *testing.T) {
	path := seeded(t)

	for _, at := range []string{"2026-11-06", "ontem"} {
		requireFailure(t, logS1(t, path, "--at", at), 1, "usage")
	}
}

func TestLogUnknownKey(t *testing.T) {
	path := seeded(t)

	got := runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S9", "--key", "replicate=1")

	requireFailure(t, got, 3, "key_not_found")
}

func TestLogRecordWithoutHistory(t *testing.T) {
	path := seeded(t)
	mustExec(t, path, "INSERT INTO rnaseq_qc (sample, replicate) VALUES ('S2', 1)")

	got := runScribo(t, "log", "--db", path, "--table", "rnaseq_qc", "--key", "sample=S2", "--key", "replicate=1")

	if lines := requireData(t, got); strings.Join(lines, "|") != historyHeader {
		t.Errorf("output = %q", got.Stdout)
	}
}

func TestLogNeverCreatesTables(t *testing.T) {
	path := newDB(t, qcTable, "INSERT INTO rnaseq_qc (sample, replicate) VALUES ('S1', 1)")
	schema := scalar(t, path, "SELECT group_concat(name || ':' || sql, ';') FROM sqlite_schema")

	requireData(t, logS1(t, path))

	if after := scalar(t, path, "SELECT group_concat(name || ':' || sql, ';') FROM sqlite_schema"); after != schema {
		t.Errorf("schema changed:\nbefore %s\nafter  %s", schema, after)
	}
}

func TestLogRecordsNoRun(t *testing.T) {
	path := threeWrites(t)
	before := count(t, path, "tool_runs")

	for i := 0; i < 3; i++ {
		requireData(t, logS1(t, path))
	}

	if after := count(t, path, "tool_runs"); after != before {
		t.Errorf("tool_runs rows went from %s to %s", before, after)
	}
}

func TestLogHelp(t *testing.T) {
	got := runScribo(t, "log", "--help")

	if got.ExitCode != 0 {
		t.Fatalf("exit %d", got.ExitCode)
	}
	for _, want := range []string{"--at", "--format json", "--field"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}
