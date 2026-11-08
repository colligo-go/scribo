package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentIngestsAndUpsertsKeepEveryChain(t *testing.T) {
	t.Parallel()
	const samples, metrics, ingests, upserters, rounds = 20, 5, 3, 5, 3
	first := syntheticReport(t, samples, metrics, 0)
	path := syntheticDB(t, first)
	requireIngested(t, runScribo(t, ingestTool(path, first)...))
	reports := make([]string, ingests)
	for i := range reports {
		reports[i] = syntheticReport(t, samples, metrics, i+1)
	}

	runs := 1
	for round := 0; round < rounds; round++ {
		var group sync.WaitGroup
		results := make([]result, ingests+upserters)
		for i := 0; i < ingests; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				args := append(ingestTool(path, reports[i]), "--busy-timeout", "30000")
				results[i] = runScriboNoFatal(args...)
			}()
		}
		for i := 0; i < upserters; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				results[ingests+i] = runScriboNoFatal("upsert", "--db", path, "--table", "big", "--busy-timeout", "30000",
					"--key", fmt.Sprintf("sample=S%06d", i), fmt.Sprintf("m0=%d.5", 9000+100*round+i))
			}()
		}
		group.Wait()
		runs += ingests + upserters
		for i, got := range results {
			if got.ExitCode != 0 || got.Stdout != "" || strings.Contains(got.Stderr, "ERROR") || strings.Contains(got.Stderr, "reconciled") {
				t.Errorf("round %d process %d: exit %d, stdout %q, stderr %q", round, i, got.ExitCode, got.Stdout, got.Stderr)
			}
		}
	}

	rows := query(t, path, `SELECT record_key, field, ifnull(old_value, 'NULL'), old_is_null, ifnull(new_value, 'NULL'), new_is_null, source
		FROM big_history ORDER BY record_key, field, changed_at, id`)
	last := map[string]string{}
	for i, row := range rows {
		if row[6] != "scribo" {
			t.Errorf("row %v is not a scribo row", row)
		}
		chain := row[0] + " " + row[1]
		if i == 0 || rows[i-1][0]+" "+rows[i-1][1] != chain {
			if row[3] != "1" {
				t.Errorf("%s: first row %v has a non-NULL old value", chain, row)
			}
		} else if previous := rows[i-1]; row[2] != previous[4] || row[3] != previous[5] {
			t.Errorf("%s: row %v does not continue %v", chain, row, previous)
		}
		last[chain] = row[4]
	}
	for s := 0; s < samples; s++ {
		sample := fmt.Sprintf("S%06d", s)
		for m := 0; m < metrics; m++ {
			field := fmt.Sprintf("m%d", m)
			stored := scalar(t, path, fmt.Sprintf(`SELECT "%s" FROM big WHERE sample = ?`, field), sample)
			chain := fmt.Sprintf(`[["sample","%s"]] %s`, sample, field)
			if want, ok := last[chain]; !ok || !sameNumber(stored, want) {
				t.Errorf("%s: table holds %s, the last history row says %q", chain, stored, want)
			}
		}
	}
	closed := scalar(t, path, "SELECT count(*) FROM tool_runs WHERE exit_code = 0 AND ended_at IS NOT NULL")
	if all := count(t, path, "tool_runs"); closed != strconv.Itoa(runs) || all != closed {
		t.Errorf("tool_runs: %s rows, %s successful and closed, want %d of each", all, closed, runs)
	}
}

func sameNumber(a, b string) bool {
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	return errA == nil && errB == nil && x == y
}
