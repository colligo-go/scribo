package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const readingRows = `INSERT INTO rnaseq_qc (sample, replicate, Alignment_Rate, Mapped_Reads, note, run_date) VALUES
	('S1', 2, 94.3, 12, NULL, '2025-12-31'),
	('S2', 1, 100, 20, 'null', '2026-02-01'),
	('S3', 1, 89.5, 8, '', '2026-03-15'),
	('S4', 2, 96, 15, 'a' || char(9) || 'b', '2025-06-01')`

const readingHeader = "sample\treplicate\tAlignment_Rate\tMapped_Reads\tnote\trun_date\n"

func readingDB(t *testing.T, extra ...string) string {
	t.Helper()
	return newDB(t, append([]string{qcTable, readingRows}, extra...)...)
}

func queryQC(t *testing.T, path string, args ...string) result {
	t.Helper()
	return runScribo(t, append([]string{"query", "--db", path, "--table", "rnaseq_qc"}, args...)...)
}

func requireOutput(t *testing.T, got result) string {
	t.Helper()
	if got.ExitCode != 0 || got.Stderr != "" {
		t.Fatalf("exit %d, stderr %q, want success", got.ExitCode, got.Stderr)
	}
	return got.Stdout
}

func samplesOf(t *testing.T, stdout string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	var samples []string
	for _, line := range lines[1:] {
		sample, _, _ := strings.Cut(line, "\t")
		samples = append(samples, sample)
	}
	return strings.Join(samples, ",")
}

func TestQueryPrintsEveryRecordInKeyOrder(t *testing.T) {
	t.Parallel()
	path := readingDB(t)

	got := requireOutput(t, queryQC(t, path))

	want := readingHeader +
		"S2\t1\t100\t20\tnull\t2026-02-01\n" +
		"S3\t1\t89.5\t8\t\t2026-03-15\n" +
		"S1\t2\t94.3\t12\t\\N\t2025-12-31\n" +
		"S4\t2\t96\t15\ta\\tb\t2025-06-01\n"
	if got != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", got, want)
	}
}

func TestQueryFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"numeric comparison", []string{"--where", "Alignment_Rate>90"}, "S2,S1,S4"},
		{"filters are joined by AND", []string{"--where", "Mapped_Reads>=12", "--where", "Alignment_Rate<95"}, "S1"},
		{"null in a value is text", []string{"--where", "note=null"}, "S2"},
		{"is null", []string{"--is-null", "note"}, "S1"},
		{"not null", []string{"--not-null", "note"}, "S2,S3,S4,S5"},
		{"not equal keeps NULL", []string{"--where", "note!=null"}, "S3,S1,S4,S5"},
		{"the value may hold an operator", []string{"--where", "note=a=b"}, "S5"},
		{"force text on a date", []string{"--where", "run_date>2026-01-01", "--force-text"}, "S2,S3"},
		{"force text on a number compares text", []string{"--where", "Alignment_Rate<9", "--force-text"}, "S2,S3"},
		{"equal on a numeric column", []string{"--where", "Mapped_Reads=08"}, "S3"},
	}
	path := readingDB(t, `INSERT INTO rnaseq_qc (sample, replicate, note) VALUES ('S5', 3, 'a=b')`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := samplesOf(t, requireOutput(t, queryQC(t, path, tt.args...))); got != tt.want {
				t.Errorf("samples = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestQueryRefusesBadFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		where   string
		exit    int
		code    string
		context string
	}{
		{"ordering on a date", "run_date>2026-01-01", 2, "untyped_comparison", "→ pass --force-text to compare as text (ISO dates sort correctly as text)\n"},
		{"a word for a number", "Alignment_Rate>abc", 3, "invalid_value", ""},
		{"a decimal comma", "Alignment_Rate>94,3", 3, "invalid_value", "→ use a dot as the decimal separator: 94.3\n"},
		{"unknown column", "nope=1", 2, "column_not_found", "→ did you mean \"note\"?\n"},
		{"empty field", "=1", 1, "usage", ""},
		{"no operator", "Alignment_Rate", 1, "usage", ""},
	}
	path := readingDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := queryQC(t, path, "--where", tt.where)
			requireFailure(t, got, tt.exit, tt.code)
			if tt.context != "" && !strings.HasSuffix(got.Stderr, tt.context) {
				t.Errorf("stderr = %q, want it to end with %q", got.Stderr, tt.context)
			}
		})
	}
	got := queryQC(t, path, "--where", "run_date>2026-01-01")
	if !strings.Contains(got.Stderr, "run_date is not a numeric column; > would compare text") {
		t.Errorf("stderr = %q", got.Stderr)
	}
}

func TestQueryFieldsAndLimit(t *testing.T) {
	t.Parallel()
	path := readingDB(t)

	got := requireOutput(t, queryQC(t, path, "--fields", "sample,Mapped_Reads", "--limit", "2"))

	if want := "sample\tMapped_Reads\nS2\t20\nS3\t8\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	repeated := requireOutput(t, queryQC(t, path, "--fields", "Mapped_Reads", "--fields", "sample", "--limit", "1"))
	if want := "Mapped_Reads\tsample\n20\tS2\n"; repeated != want {
		t.Errorf("stdout = %q, want %q", repeated, want)
	}
	for _, args := range [][]string{
		{"--limit", "0"}, {"--limit", "-1"}, {"--fields", "sample,sample"}, {"--fields", "sample,"}, {"--format", "xml"}, {"--is-null", ""},
	} {
		requireFailure(t, queryQC(t, path, args...), 1, "usage")
	}
}

func TestQueryCSV(t *testing.T) {
	t.Parallel()
	path := readingDB(t, `INSERT INTO rnaseq_qc (sample, replicate, note) VALUES ('S5', 1, 'x,y')`,
		`UPDATE rnaseq_qc SET run_date = 'q"t' WHERE sample = 'S5'`)

	got := requireOutput(t, queryQC(t, path, "--format", "csv"))

	want := "sample,replicate,Alignment_Rate,Mapped_Reads,note,run_date\n" +
		"S2,1,100,20,null,2026-02-01\n" +
		"S3,1,89.5,8,\"\",2026-03-15\n" +
		"S5,1,,,\"x,y\",\"q\"\"t\"\n" +
		"S1,2,94.3,12,,2025-12-31\n" +
		"S4,2,96,15,a\tb,2025-06-01\n"
	if got != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", got, want)
	}
}

func TestQueryJSON(t *testing.T) {
	t.Parallel()
	path := readingDB(t, `INSERT INTO rnaseq_qc (sample, replicate, note) VALUES ('S5', 3, '<&>')`)

	got := requireOutput(t, queryQC(t, path, "--format", "json"))

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5: %q", len(lines), got)
	}
	if want := `{"sample":"S2","replicate":"1","Alignment_Rate":"100","Mapped_Reads":"20","note":"null","run_date":"2026-02-01"}`; lines[0] != want {
		t.Errorf("first line = %s, want %s", lines[0], want)
	}
	if want := `{"sample":"S5","replicate":"3","Alignment_Rate":null,"Mapped_Reads":null,"note":"<&>","run_date":null}`; lines[4] != want {
		t.Errorf("last line = %s, want %s", lines[4], want)
	}
	notes := map[string]any{}
	for _, line := range lines {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		notes[object["sample"].(string)] = object["note"]
	}
	if notes["S1"] != nil || notes["S2"] != "null" || notes["S3"] != "" || notes["S4"] != "a\tb" {
		t.Errorf("notes = %#v", notes)
	}
}

func TestQueryPretty(t *testing.T) {
	t.Parallel()
	path := readingDB(t, `INSERT INTO rnaseq_qc (sample, replicate, note) VALUES ('S5', 3, 'café')`)

	got := requireOutput(t, queryQC(t, path, "--fields", "note,sample", "--format", "pretty"))

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("got %d lines, want 7:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[1], "----") || !strings.Contains(got, "∅") || !strings.Contains(got, `a\tb`) {
		t.Errorf("pretty output:\n%s", got)
	}
	column := utf8.RuneCountInString(lines[0][:strings.Index(lines[0], "sample")])
	for _, line := range lines[2:] {
		at := strings.LastIndex(line, "S")
		if position := utf8.RuneCountInString(line[:at]); position != column {
			t.Errorf("line %q puts the sample at %d, the header at %d", line, position, column)
		}
	}
}

func TestQueryOrdersByRowidWithoutAUniqueKey(t *testing.T) {
	t.Parallel()
	path := newDB(t, `CREATE TABLE plain (a TEXT, b INTEGER)`, `INSERT INTO plain VALUES ('z', 1), ('a', 2), ('m', 3)`)

	first := requireOutput(t, runScribo(t, "query", "--db", path, "--table", "plain"))
	second := requireOutput(t, runScribo(t, "query", "--db", path, "--table", "plain"))

	if want := "a\tb\nz\t1\na\t2\nm\t3\n"; first != want || second != want {
		t.Errorf("first %q, second %q, want %q", first, second, want)
	}
}

func TestQueryAndExportRefuseReservedAndMissingTables(t *testing.T) {
	t.Parallel()
	path := seeded(t)
	for _, command := range []string{"query", "export"} {
		requireFailure(t, runScribo(t, command, "--db", path, "--table", "tool_runs"), 2, "reserved_table")
		requireFailure(t, runScribo(t, command, "--db", path, "--table", "rnaseq"), 2, "table_not_found")
	}
}

func TestReadingNeverWrites(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{"fresh": readingDB(t), "seeded": seeded(t)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			before := snapshot(t, path, "sqlite_schema", "rnaseq_qc")
			requireOutput(t, queryQC(t, path, "--where", "Mapped_Reads>1"))
			requireOutput(t, runScribo(t, "export", "--db", path, "--table", "rnaseq_qc", "--format", "json"))
			requireOutput(t, runScribo(t, "export", "--db", path, "--table", "rnaseq_qc", "--out", filepath.Join(t.TempDir(), "x.tsv")))
			if after := snapshot(t, path, "sqlite_schema", "rnaseq_qc"); after != before {
				t.Errorf("database changed:\nbefore %s\nafter  %s", before, after)
			}
			if name == "seeded" {
				if got := count(t, path, "tool_runs"); got != "1" {
					t.Errorf("tool_runs rows = %s, want the seed's 1", got)
				}
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestExportWritesTheFileQueryWouldPrint(t *testing.T) {
	t.Parallel()
	path := readingDB(t)
	out := filepath.Join(t.TempDir(), "qc.tsv")
	export := func(extra ...string) result {
		return runScribo(t, append([]string{"export", "--db", path, "--table", "rnaseq_qc", "--out", out}, extra...)...)
	}
	printed := requireOutput(t, queryQC(t, path))

	requireSuccess(t, export())
	if got := readFile(t, out); got != printed {
		t.Errorf("file = %q, want the query output %q", got, printed)
	}
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := export()
	requireFailure(t, got, 4, "file_exists")
	if !strings.Contains(got.Stderr, "→ pass --overwrite to replace it") || readFile(t, out) != "old" {
		t.Errorf("stderr %q, file %q, want the hint and the file intact", got.Stderr, readFile(t, out))
	}
	requireSuccess(t, export("--overwrite"))
	if got := readFile(t, out); got != printed {
		t.Errorf("overwritten file = %q, want %q", got, printed)
	}
	stdout := requireOutput(t, runScribo(t, "export", "--db", path, "--table", "rnaseq_qc"))
	if stdout != printed {
		t.Errorf("export to stdout = %q, want %q", stdout, printed)
	}
}

func requireOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("%s holds %v, want %v", dir, names, want)
	}
}

func TestExportToAReadOnlyDirectoryLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	path := readingDB(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "qc.tsv")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	for _, out := range []string{existing, filepath.Join(dir, "new.tsv")} {
		got := runScribo(t, "export", "--db", path, "--table", "rnaseq_qc", "--out", out, "--overwrite")
		requireFailure(t, got, 5, "permission_denied")
	}
	if got := readFile(t, existing); got != "old" {
		t.Errorf("existing file = %q, want it intact", got)
	}
	requireOnlyEntries(t, dir, "qc.tsv")
}

func TestExportFailingRenameRemovesTheTemporaryFile(t *testing.T) {
	t.Parallel()
	path := readingDB(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "taken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taken", "inside"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := runScribo(t, "export", "--db", path, "--table", "rnaseq_qc", "--out", filepath.Join(dir, "taken"), "--overwrite")

	if got.ExitCode == 0 {
		t.Fatalf("export over a directory succeeded: %q", got.Stdout)
	}
	requireOnlyEntries(t, dir, "taken")
}
