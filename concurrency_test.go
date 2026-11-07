package main

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
)

const (
	concurrentWorkers = 8
	writesPerWorker   = 5
)

func TestConcurrentWritersKeepTheHistoryChain(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)
	requireSuccess(t, upsertS1(t, path, "Mapped_Reads=1"))

	results := make([][]result, concurrentWorkers)
	var group sync.WaitGroup
	for worker := 0; worker < concurrentWorkers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < writesPerWorker; i++ {
				value := strconv.Itoa(1000*(worker+1) + i)
				results[worker] = append(results[worker], runScriboNoFatal(
					"upsert", "--db", path, "--table", "rnaseq_qc", "--busy-timeout", "30000",
					"--key", "sample=S1", "--key", "replicate=1", "Mapped_Reads="+value))
			}
		}()
	}
	group.Wait()

	written := map[string]bool{"1": true}
	for worker, runs := range results {
		for i, got := range runs {
			if got.ExitCode != 0 || got.Stderr != "" || got.Stdout != "" {
				t.Errorf("worker %d write %d: exit %d, stdout %q, stderr %q", worker, i, got.ExitCode, got.Stdout, got.Stderr)
			}
			written[strconv.Itoa(1000*(worker+1)+i)] = true
		}
	}

	rows := query(t, path, `SELECT id, ifnull(old_value, 'NULL'), old_is_null, new_value, changed_at, source
		FROM rnaseq_qc_history WHERE field = 'Mapped_Reads' ORDER BY changed_at, id`)
	total := concurrentWorkers*writesPerWorker + 1
	if len(rows) != total {
		t.Fatalf("Mapped_Reads history has %d rows, want %d", len(rows), total)
	}
	if rows[0][2] != "1" {
		t.Errorf("first row %v has a non-NULL old value", rows[0])
	}
	seen := map[string]bool{}
	for i, row := range rows {
		seen[row[3]] = true
		if row[5] != "scribo" {
			t.Errorf("row %v is not a scribo row", row)
		}
		if i > 0 && row[1] != rows[i-1][3] {
			t.Errorf("row %v: old_value %s, but the previous row wrote %s", row, row[1], rows[i-1][3])
		}
	}
	if fmt.Sprint(sortedKeys(seen)) != fmt.Sprint(sortedKeys(written)) {
		t.Errorf("new values %v, want exactly the written %v", sortedKeys(seen), sortedKeys(written))
	}
	if stored := scalar(t, path, "SELECT Mapped_Reads FROM rnaseq_qc"); stored != rows[len(rows)-1][3] {
		t.Errorf("table holds %s, the last history row says %s", stored, rows[len(rows)-1][3])
	}

	byID := query(t, path, "SELECT changed_at FROM rnaseq_qc_history ORDER BY id")
	for i := 1; i < len(byID); i++ {
		if byID[i][0] < byID[i-1][0] {
			t.Errorf("changed_at goes back from %s to %s as id grows", byID[i-1][0], byID[i][0])
		}
	}
	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history WHERE source = 'reconciled'"); got != "0" {
		t.Errorf("reconciled rows = %s, want 0", got)
	}
	runs := scalar(t, path, "SELECT count(*) FROM tool_runs WHERE exit_code = 0 AND ended_at IS NOT NULL")
	if all := count(t, path, "tool_runs"); runs != strconv.Itoa(total) || all != runs {
		t.Errorf("tool_runs: %s rows, %s successful and closed, want %d of each", all, runs, total)
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
