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
	return readmeSection(t, "Quick start")
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

func readmeSection(t *testing.T, heading string) []string {
	t.Helper()
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, afterHeading, found := strings.Cut(string(readme), "## "+heading)
	if !found {
		t.Fatalf("README.md has no %q section", heading)
	}
	_, afterFence, found := strings.Cut(afterHeading, "```sh\n")
	if !found {
		t.Fatalf("the %q section has no sh block", heading)
	}
	block, _, found := strings.Cut(afterFence, "```")
	if !found {
		t.Fatalf("the sh block of %q is not closed", heading)
	}
	return strings.Split(strings.TrimSpace(block), "\n")
}

func TestReadmeMultiQCRuns(t *testing.T) {
	t.Parallel()
	lines := readmeSection(t, "From a MultiQC report")
	if len(lines) != 4 {
		t.Fatalf("the MultiQC section has %d lines, want 4", len(lines))
	}
	command := func(line string) []string {
		args := strings.Fields(line)[1:]
		for i := range args {
			if args[i] == "multiqc_data.json" {
				args[i] = multiqcFixture
			}
		}
		return args
	}

	schemaLine, redirected, found := strings.Cut(lines[0], " > ")
	if !found || lines[1] != "sqlite3 study.db < "+redirected {
		t.Fatalf("lines %q and %q are not schema > FILE and sqlite3 < FILE", lines[0], lines[1])
	}
	ddl := runScribo(t, command(schemaLine)...)
	if ddl.ExitCode != 0 {
		t.Fatalf("%q exited %d, stderr %q", schemaLine, ddl.ExitCode, ddl.Stderr)
	}
	path := newDB(t, ddl.Stdout)

	var output string
	for _, line := range lines[2:] {
		args := command(line)
		for i := range args {
			if i > 0 && args[i-1] == "--db" {
				args[i] = path
			}
		}
		got := runScribo(t, args...)
		if got.ExitCode != 0 || got.Stderr != "" {
			t.Fatalf("%q exited %d, stderr %q", line, got.ExitCode, got.Stderr)
		}
		output = got.Stdout
	}
	if !strings.Contains(output, "HG001         0.23.4        619883908                     593374960") {
		t.Errorf("query output = %q", output)
	}
}
