package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
)

const (
	generalGolden = "testdata/multiqc/general.sql"
	fastpGolden   = "testdata/multiqc/fastp.sql"
)

func golden(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func uniqueKeysOf(t *testing.T, path, table string) [][]string {
	t.Helper()
	backend := sqlite.New()
	if err := backend.Open(context.Background(), path, db.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	keys, err := backend.UniqueKeys(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestSchemaFromMultiQCMatchesTheGoldenFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		golden string
		args   []string
	}{
		{generalGolden, []string{"--table", "qc"}},
		{fastpGolden, []string{"--table", "fastp_qc", "--module", "fastp"}},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			t.Parallel()
			got := runScribo(t, append([]string{"schema", "--from-multiqc", multiqcFixture}, tt.args...)...)

			if got.ExitCode != 0 || got.Stderr != "" {
				t.Fatalf("exit %d, stderr %q", got.ExitCode, got.Stderr)
			}
			if want := golden(t, tt.golden); got.Stdout != want {
				t.Errorf("stdout differs from %s byte for byte:\n%s", tt.golden, got.Stdout)
			}
		})
	}
}

func TestGoldenSchemasCreateStrictTablesKeyedBySample(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ golden, table string }{{generalGolden, "qc"}, {fastpGolden, "fastp_qc"}} {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()
			path := newDB(t, golden(t, tt.golden))

			if got := scalar(t, path, "SELECT strict FROM pragma_table_list WHERE name = ?", tt.table); got != "1" {
				t.Errorf("strict = %s, want 1", got)
			}
			if got := fmt.Sprint(uniqueKeysOf(t, path, tt.table)); got != "[[sample]]" {
				t.Errorf("unique keys = %s, want [[sample]]", got)
			}
		})
	}
}

func TestSchemaFromTSV(t *testing.T) {
	t.Parallel()
	tsv := writeTemp(t, "results.tsv", "sample\treplicate\tAlignment_Rate\tMapped_Reads\tnote\n"+
		"S1\t1\t94.3\t12\tok\n"+
		"S2\t2\t90\t\t\\N\n")

	got := runScribo(t, "schema", "--from", tsv, "--table", "rnaseq_qc", "--key", "sample", "--key", "replicate")

	want := `CREATE TABLE "rnaseq_qc" (
  "sample" TEXT NOT NULL,
  "replicate" REAL NOT NULL,
  "Alignment_Rate" REAL,
  "Mapped_Reads" REAL,
  "note" TEXT,
  UNIQUE ("sample", "replicate")
) STRICT;
`
	if got.ExitCode != 0 || got.Stderr != "" || got.Stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", got.ExitCode, got.Stderr, got.Stdout)
	}
	path := newDB(t, got.Stdout)
	if keys := fmt.Sprint(uniqueKeysOf(t, path, "rnaseq_qc")); keys != "[[replicate sample]]" {
		t.Errorf("unique keys = %s", keys)
	}
}

func TestSchemaFromTSVWithOnlyAHeader(t *testing.T) {
	t.Parallel()
	tsv := writeTemp(t, "empty.tsv", "sample\tvalue\r\n")

	got := runScribo(t, "schema", "--from", tsv, "--table", "t", "--key", "sample")

	if got.ExitCode != 0 || !strings.Contains(got.Stdout, `"value" TEXT,`) || strings.Contains(got.Stdout, "\r") {
		t.Errorf("exit %d, stderr %q, stdout %q", got.ExitCode, got.Stderr, got.Stdout)
	}
}

func TestSchemaRefusals(t *testing.T) {
	t.Parallel()
	tsv := writeTemp(t, "results.tsv", "sample\tvalue\nS1\t1\nS2\t2\t3\n")
	duplicated := writeTemp(t, "duplicated.tsv", "sample\tValue\tvalue\nS1\t1\t2\n")
	notJSON := writeTemp(t, "multiqc_data.json", `{"report_general_stats_data": `)
	missing := filepath.Join(t.TempDir(), "missing.json")
	empty := writeTemp(t, "empty.tsv", "")
	unnamed := writeTemp(t, "unnamed.tsv", "sample\t\tvalue\nS1\t1\t2\n")
	colliding := writeTemp(t, "multiqc_data.json", `{
	  "report_general_stats_headers": {"tool": {"Reads": {"namespace": "Tool"}, "reads": {}}},
	  "report_general_stats_data": {"tool": {"S1": {"Reads": 1, "reads": 2}}}
	}`)
	directory := t.TempDir()
	tests := []struct {
		name     string
		args     []string
		exit     int
		code     string
		mentions []string
	}{
		{"tsv without --key", []string{"--from", tsv, "--table", "t"}, 1, "usage", []string{"--key is required"}},
		{"key outside the header", []string{"--from", tsv, "--table", "t", "--key", "sampel"}, 2, "column_not_found", []string{"sampel", `did you mean "sample"?`}},
		{"line with an extra cell", []string{"--from", tsv, "--table", "t", "--key", "sample"}, 4, "invalid_format", []string{"line 3 has 3 cells and the header has 2"}},
		{"columns equal but for case", []string{"--from", duplicated, "--table", "t", "--key", "sample"}, 4, "invalid_format", []string{"Value and value"}},
		{"unknown module", []string{"--from-multiqc", multiqcFixture, "--table", "t", "--module", "nope"}, 4, "module_not_found", []string{"modules: cutadapt, fastp, fastqc, salmon, star"}},
		{"invalid JSON", []string{"--from-multiqc", notJSON, "--table", "t"}, 4, "invalid_format", []string{"ends early"}},
		{"missing file", []string{"--from-multiqc", missing, "--table", "t"}, 4, "file_not_found", []string{"missing.json"}},
		{"no source", []string{"--table", "t"}, 1, "usage", nil},
		{"both sources", []string{"--from", tsv, "--from-multiqc", multiqcFixture, "--table", "t", "--key", "sample"}, 1, "usage", nil},
		{"key with multiqc", []string{"--from-multiqc", multiqcFixture, "--table", "t", "--key", "sample"}, 1, "usage", nil},
		{"module with tsv", []string{"--from", tsv, "--table", "t", "--key", "sample", "--module", "fastp"}, 1, "usage", nil},
		{"no table", []string{"--from-multiqc", multiqcFixture}, 1, "usage", []string{"--table"}},
		{"reserved table", []string{"--from-multiqc", multiqcFixture, "--table", "tool_runs"}, 2, "reserved_table", nil},
		{"empty module", []string{"--from-multiqc", multiqcFixture, "--table", "t", "--module", ""}, 1, "usage", []string{"--module needs a module name"}},
		{"empty key", []string{"--from", tsv, "--table", "t", "--key", ""}, 1, "usage", []string{"--key needs a column name"}},
		{"key twice", []string{"--from", tsv, "--table", "t", "--key", "sample", "--key", "sample"}, 1, "usage", []string{`key column "sample" is given twice`}},
		{"empty tsv", []string{"--from", empty, "--table", "t", "--key", "sample"}, 4, "invalid_format", []string{"the file is empty"}},
		{"column without a name", []string{"--from", unnamed, "--table", "t", "--key", "sample"}, 4, "invalid_format", []string{"empty name"}},
		{"metrics equal but for case", []string{"--from-multiqc", colliding, "--table", "t", "--module", "tool"}, 4, "invalid_format", []string{"Reads and reads name the same column"}},
		{"tsv is a directory", []string{"--from", directory, "--table", "t", "--key", "sample"}, 5, "io_error", []string{"cannot read " + directory}},
		{"report is a directory", []string{"--from-multiqc", directory, "--table", "t"}, 5, "io_error", []string{"cannot read " + directory}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := runScribo(t, append([]string{"schema"}, tt.args...)...)

			requireFailure(t, got, tt.exit, tt.code)
			for _, mention := range tt.mentions {
				if !strings.Contains(got.Stderr, mention) {
					t.Errorf("stderr = %q, want it to mention %q", got.Stderr, mention)
				}
			}
		})
	}
}

func TestSchemaNeverOpensTheDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing", "study.db")

	got := runScribo(t, "schema", "--db", path, "--from-multiqc", multiqcFixture, "--table", "qc")

	if got.ExitCode != 0 || got.Stdout != golden(t, generalGolden) {
		t.Fatalf("exit %d, stderr %q", got.ExitCode, got.Stderr)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("schema created %s", filepath.Dir(path))
	}
}

func TestMissingTableNamesScriboSchema(t *testing.T) {
	t.Parallel()
	path := newDB(t, qcTable)

	got := runScribo(t, "upsert", "--db", path, "--table", "nope", "--key", "sample=S1", "note=x")

	requireFailure(t, got, 2, "table_not_found")
	if !strings.Contains(got.Stderr, "→ tables: rnaseq_qc; scribo schema prints the CREATE TABLE of a new one\n") {
		t.Errorf("stderr = %q, want the context to name scribo schema", got.Stderr)
	}
}
