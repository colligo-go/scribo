package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

const illustrativeMoment = "2026-11-07T12:00:00Z"

func readmeQuickStart(t *testing.T) []string {
	t.Helper()
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, afterHeading, found := strings.Cut(string(readme), "## Quick start")
	if !found {
		t.Fatal("README.md has no \"Quick start\" section")
	}
	_, afterFence, found := strings.Cut(afterHeading, "```sh\n")
	if !found {
		t.Fatal("the quick start has no sh block")
	}
	block, _, found := strings.Cut(afterFence, "```")
	if !found {
		t.Fatal("the sh block of the quick start is not closed")
	}
	return strings.Split(strings.TrimSpace(block), "\n")
}

func TestReadmeQuickStartRuns(t *testing.T) {
	t.Parallel()
	createTable := regexp.MustCompile(`^sqlite3 \S+ "(.+)"$`)
	var path string
	scriboLines := 0

	for _, line := range readmeQuickStart(t) {
		if match := createTable.FindStringSubmatch(line); match != nil {
			path = newDB(t, match[1])
			continue
		}
		if !strings.HasPrefix(line, "scribo ") {
			t.Fatalf("quick start line %q is neither the sqlite3 line nor a scribo command", line)
		}
		if path == "" {
			t.Fatal("the quick start runs scribo before creating the table")
		}
		args := strings.Fields(line)[1:]
		for i := range args {
			if args[i] == illustrativeMoment {
				args[i] = time.Now().UTC().Add(time.Second).Format(time.RFC3339)
			}
			if i > 0 && args[i-1] == "--db" {
				args[i] = path
			}
		}
		scriboLines++
		got := runScribo(t, args...)
		if got.ExitCode != 0 {
			t.Errorf("%q exited %d, stderr %q", line, got.ExitCode, got.Stderr)
		}
	}
	if scriboLines != 4 {
		t.Errorf("the quick start has %d scribo lines, want 4", scriboLines)
	}
	if got := scalar(t, path, "SELECT count(*) FROM rnaseq_qc_history"); got != "3" {
		t.Errorf("history rows = %s, want 3 (two fields, then one change)", got)
	}
}
