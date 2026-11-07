package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogPretty(t *testing.T) {
	t.Parallel()
	path := threeWrites(t)

	lines := requireData(t, logS1(t, path, "--format", "pretty"))

	if fields := strings.Fields(lines[0]); strings.Join(fields, " ") != "changed_at run_id source field old_value new_value" {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "---") || len(lines) != 7 {
		t.Fatalf("pretty:\n%s", strings.Join(lines, "\n"))
	}
	nulls := 0
	for _, line := range lines[2:] {
		if strings.Contains(line, "∅") {
			nulls++
		}
	}
	if nulls != 3 {
		t.Errorf("%d lines show ∅, want the 3 of the first write", nulls)
	}
	column := strings.Index(lines[0], "source")
	for _, line := range lines[2:] {
		if at := strings.Index(line, "scribo"); at != column {
			t.Errorf("line %q puts the source at %d, the header at %d", line, at, column)
		}
	}
	state := requireData(t, logS1(t, path, "--format", "pretty", "--at", "run:2"))
	if strings.Join(strings.Fields(state[0]), " ") != "field value" || len(state) != 5 {
		t.Errorf("state:\n%s", strings.Join(state, "\n"))
	}
}

func TestLogWithRuns(t *testing.T) {
	t.Parallel()
	path := threeWrites(t)
	version := scalar(t, path, "SELECT tool_version FROM tool_runs WHERE id = 1")

	lines := requireData(t, logS1(t, path, "--with-runs"))

	if lines[0] != historyHeader+"\ttool_version\tstep\texit_code" {
		t.Errorf("header = %q", lines[0])
	}
	for _, line := range lines[1:] {
		cells := strings.Split(line, "\t")
		if len(cells) != 11 || cells[8] != version || cells[9] != "" || cells[10] != "0" {
			t.Errorf("line %q, want the version %s, an empty step and exit_code 0", line, version)
		}
	}

	for _, line := range requireData(t, logS1(t, path, "--with-runs", "--format", "json")) {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if object["tool_version"] != version || object["step"] != "" || object["exit_code"] != float64(0) {
			t.Errorf("line %s", line)
		}
	}
	pretty := requireData(t, logS1(t, path, "--with-runs", "--format", "pretty"))
	if !strings.HasSuffix(strings.Join(strings.Fields(pretty[0]), " "), "tool_version step exit_code") {
		t.Errorf("pretty header = %q", pretty[0])
	}

	requireFailure(t, logS1(t, path, "--with-runs", "--at", "run:1"), 1, "usage")
}

func TestLogWithRunsShowsAnOpenRunAsEmpty(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	mustExec(t, path, "UPDATE tool_runs SET exit_code = NULL, ended_at = NULL")

	lines := requireData(t, logS1(t, path, "--with-runs"))

	if cells := strings.Split(lines[1], "\t"); cells[10] != "" {
		t.Errorf("exit_code = %q, want empty", cells[10])
	}
}

func TestLogAtRun(t *testing.T) {
	t.Parallel()
	path := threeWrites(t)

	lines := requireData(t, logS1(t, path, "--at", "run:2"))

	want := "field\tvalue\tis_null\nAlignment_Rate\t94.3\t0\nMapped_Reads\t13\t0\nnote\tok\t0"
	if got := strings.Join(lines, "\n"); got != want {
		t.Errorf("state:\n%s\nwant:\n%s", got, want)
	}
	if got := requireData(t, logS1(t, path, "--at", "run:1", "--field", "Mapped_Reads")); strings.Join(got, "\n") != "field\tvalue\tis_null\nMapped_Reads\t12\t0" {
		t.Errorf("run:1 state = %q", got)
	}
}

func TestLogAtRunCutsByIDWithinOneMoment(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	recordKey := scalar(t, path, "SELECT record_key FROM rnaseq_qc_history LIMIT 1")
	moment := "2099-01-01T00:00:00.000Z"
	mustExec(t, path, `INSERT INTO rnaseq_qc_history (run_id, record_key, field, old_value, old_is_null, new_value, new_is_null, source, changed_at)
		VALUES (20, ?, 'Mapped_Reads', '12', 0, '50', 0, 'scribo', ?), (21, ?, 'Mapped_Reads', '50', 0, '60', 0, 'scribo', ?)`,
		recordKey, moment, recordKey, moment)

	lines := requireData(t, logS1(t, path, "--at", "run:20", "--field", "Mapped_Reads"))

	if got := strings.Join(lines, "\n"); got != "field\tvalue\tis_null\nMapped_Reads\t50\t0" {
		t.Errorf("state = %q, want Mapped_Reads 50: run 21 shares the moment but comes after", got)
	}
}

func TestLogAtRunRefusals(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable, `CREATE TABLE other_qc (k TEXT, v INTEGER, UNIQUE(k))`)
	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=1"))
	requireSuccess(t, runScribo(t, "upsert", "--db", path, "--table", "other_qc", "--key", "k=a", "v=1"))

	got := logS1(t, path, "--at", "run:99")
	requireFailure(t, got, 3, "run_not_found")
	if !strings.Contains(got.Stderr, "run 99 wrote nothing to rnaseq_qc") {
		t.Errorf("stderr = %q", got.Stderr)
	}
	requireFailure(t, logS1(t, path, "--at", "run:2"), 3, "run_not_found")
	for _, at := range []string{"run:0", "run:x", "run:-1", "run:"} {
		requireFailure(t, logS1(t, path, "--at", at), 1, "usage")
	}
}
