package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
)

func decodeFixture(t *testing.T) map[string]any {
	t.Helper()
	content, err := os.ReadFile(multiqcFixture)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var top map[string]any
	if err := decoder.Decode(&top); err != nil {
		t.Fatal(err)
	}
	return top
}

func object(t *testing.T, node any, path ...string) map[string]any {
	t.Helper()
	for _, name := range path {
		parent, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object", path)
		}
		node = parent[name]
	}
	result, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("%v is not an object", path)
	}
	return result
}

func alteredFixture(t *testing.T, group, sample, field, value string) string {
	t.Helper()
	top := decodeFixture(t)
	object(t, top, "report_general_stats_data", group, sample)[field] = json.Number(value)
	content, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, "multiqc_data.json", string(content))
}

func ingest(t *testing.T, path, table, file string, args ...string) result {
	t.Helper()
	base := []string{"ingest", "multiqc", "--db", path, "--table", table, file}
	return runScribo(t, append(base, args...)...)
}

func schemaFor(t *testing.T, table string, args ...string) string {
	t.Helper()
	got := runScribo(t, append([]string{"schema", "--from-multiqc", multiqcFixture, "--table", table}, args...)...)
	if got.ExitCode != 0 {
		t.Fatalf("schema: exit %d, stderr %q", got.ExitCode, got.Stderr)
	}
	return got.Stdout
}

func tableColumns(t *testing.T, path, table string) map[string]db.Column {
	t.Helper()
	backend := sqlite.New()
	if err := backend.Open(context.Background(), path, db.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	columns, err := backend.Columns(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]db.Column{}
	for _, column := range columns {
		byName[column.Name] = column
	}
	return byName
}

func storedRows(t *testing.T, path, table string) map[string]map[string]db.Value {
	t.Helper()
	handle, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	rows, err := handle.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]map[string]db.Value{}
	for rows.Next() {
		cells := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		row := map[string]db.Value{}
		for i, name := range names {
			value, err := db.Render(cells[i])
			if err != nil {
				t.Fatal(err)
			}
			row[name] = value
		}
		stored[row["sample"].Raw] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return stored
}

func jsonText(value any) (db.Value, bool) {
	switch v := value.(type) {
	case json.Number:
		return db.Value{Raw: v.String()}, true
	case string:
		return db.Value{Raw: v}, true
	case bool:
		if v {
			return db.Value{Raw: "true"}, true
		}
		return db.Value{Raw: "false"}, true
	}
	return db.Value{}, false
}

func requireWarnings(t *testing.T, got result, codes ...string) {
	t.Helper()
	if got.ExitCode != 0 || got.Stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", got.ExitCode, got.Stdout, got.Stderr)
	}
	if n := strings.Count(got.Stderr, "WARNING ["); n != len(codes) {
		t.Errorf("stderr = %q, want %d warnings", got.Stderr, len(codes))
	}
	for _, code := range codes {
		if strings.Count(got.Stderr, "WARNING ["+code+"]") != 1 {
			t.Errorf("stderr = %q, want one %s warning", got.Stderr, code)
		}
	}
}

func TestIngestGeneralStatsWritesEveryValueOfTheReport(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))

	requireSuccess(t, ingest(t, path, "qc", multiqcFixture))

	top := decodeFixture(t)
	headers := object(t, top, "report_general_stats_headers")
	data := object(t, top, "report_general_stats_data")
	columns := tableColumns(t, path, "qc")
	stored := storedRows(t, path, "qc")
	samples := map[string]bool{}
	compared := 0
	for group, metrics := range headers {
		for metric, header := range metrics.(map[string]any) {
			module := strings.ToLower(header.(map[string]any)["namespace"].(string))
			column := columns[module+"."+metric]
			for sample, fields := range object(t, data, group) {
				samples[sample] = true
				raw, ok := jsonText(fields.(map[string]any)[metric])
				if !ok {
					continue
				}
				want, err := db.Canonical(column, raw)
				if err != nil {
					t.Fatalf("%s of %s: %v", column.Name, sample, err)
				}
				if got := stored[sample][column.Name]; got != want {
					t.Errorf("%s of %s = %+v, want %+v", column.Name, sample, got, want)
				}
				compared++
			}
		}
	}
	if len(stored) != len(samples) || len(stored) != 9 {
		t.Errorf("%d records for %d samples", len(stored), len(samples))
	}
	if compared < 40 {
		t.Errorf("compared only %d values", compared)
	}
	if got := query(t, path, "SELECT count(*), exit_code FROM tool_runs"); got[0][0] != "1" || got[0][1] != "0" {
		t.Errorf("tool_runs = %v, want one run with exit 0", got)
	}
}

func TestIngestTwiceAddsARunButNoHistory(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))
	requireSuccess(t, ingest(t, path, "qc", multiqcFixture))
	before := count(t, path, "qc_history")

	requireSuccess(t, ingest(t, path, "qc", multiqcFixture))

	if after := count(t, path, "qc_history"); after != before {
		t.Errorf("history rows %s → %s, want no new row", before, after)
	}
	if got := query(t, path, "SELECT exit_code FROM tool_runs ORDER BY id"); len(got) != 2 || got[1][0] != "0" {
		t.Errorf("tool_runs = %v", got)
	}
}

func TestIngestOfOneChangedValueAddsOneHistoryRow(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))
	requireSuccess(t, ingest(t, path, "qc", multiqcFixture))
	before := count(t, path, "qc_history")

	requireSuccess(t, ingest(t, path, "qc", alteredFixture(t, "cutadapt", "SRR1067505_1", "percent_trimmed", "2.5")))

	added := query(t, path, "SELECT record_key, field, new_value FROM qc_history ORDER BY id LIMIT -1 OFFSET "+before)
	want := `[["sample","SRR1067505_1"]] cutadapt.percent_trimmed 2.5`
	if len(added) != 1 || strings.Join(added[0], " ") != want {
		t.Errorf("added rows = %v, want one: %s", added, want)
	}
}

func TestIngestFastpModule(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, fastpGolden))

	requireSuccess(t, ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp"))

	rows := query(t, path, `SELECT sample, tool_version, before_filtering_total_reads >= after_filtering_total_reads FROM fastp_qc ORDER BY sample`)
	want := "[[HG001 0.23.4 1] [HG002_son 0.23.4 1] [HG003_father 0.23.4 1] [HG004_mother 0.23.4 1]]"
	if got := fmtRows(rows); got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
	got := runScribo(t, "query", "--db", path, "--table", "fastp_qc", "--where", "sample=HG001", "--fields", "after_filtering_total_reads")
	if got.Stdout != "after_filtering_total_reads\n593374960\n" {
		t.Errorf("query stdout = %q, want the count in plain decimal", got.Stdout)
	}
}

func fmtRows(rows [][]string) string {
	parts := make([]string, len(rows))
	for i, row := range rows {
		parts[i] = "[" + strings.Join(row, " ") + "]"
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func TestIngestRefusesAMissingColumnAndWritesNothing(t *testing.T) {
	t.Parallel()
	ddl := strings.Replace(golden(t, fastpGolden), "  \"pct_surviving\" REAL,\n", "", 1)
	ddl = strings.Replace(ddl, "  \"pct_duplication\" REAL,\n", "", 1)
	path := newDB(t, ddl)
	before := snapshot(t, path, "fastp_qc")

	got := ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp")

	requireFailure(t, got, 2, "column_not_found")
	for _, want := range []string{"2 columns do not exist in fastp_qc: pct_duplication, pct_surviving", "→ regenerate the table with scribo schema or add the columns with ALTER TABLE"} {
		if !strings.Contains(got.Stderr, want) {
			t.Errorf("stderr = %q, want %q", got.Stderr, want)
		}
	}
	if after := snapshot(t, path, "fastp_qc"); after != before {
		t.Errorf("table changed: %s", after)
	}
	if n := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name IN ('tool_runs', 'fastp_qc_history')"); n != "0" {
		t.Errorf("%s contract or history tables created", n)
	}
}

func TestIngestRefusesAValueOfTheLastSampleAndWritesNoSample(t *testing.T) {
	t.Parallel()
	ddl := strings.Replace(schemaFor(t, "cutadapt_qc", "--module", "cutadapt"), `"bp_processed" REAL`, `"bp_processed" INTEGER`, 1)
	path := newDB(t, ddl)
	if got := tableColumns(t, path, "cutadapt_qc")["bp_processed"].DeclaredType; got != "INTEGER" {
		t.Fatalf("bp_processed is %s, want INTEGER in the hand-edited table", got)
	}
	altered := alteredFixture(t, "cutadapt", "SRR1067505_1", "bp_processed", "1104515280.5")
	before := snapshot(t, path, "cutadapt_qc")

	got := ingest(t, path, "cutadapt_qc", altered, "--module", "cutadapt")

	requireFailure(t, got, 3, "invalid_value")
	if !strings.Contains(got.Stderr, `sample SRR1067505_1, metric "bp_processed": `) {
		t.Errorf("stderr = %q, want the sample and the metric", got.Stderr)
	}
	if after := snapshot(t, path, "cutadapt_qc"); after != before || count(t, path, "cutadapt_qc") != "0" {
		t.Errorf("a sample was written: %s", after)
	}
	if n := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name = 'tool_runs'"); n != "0" {
		t.Error("tool_runs created by a write refused before its run")
	}
}

func TestIngestFailingOnTheThirdSampleRollsBackEverySample(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, fastpGolden), `CREATE TRIGGER refuse_third BEFORE INSERT ON fastp_qc
		WHEN NEW.sample = 'HG003_father' BEGIN SELECT RAISE(ABORT, 'refused'); END`)

	got := ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp")

	requireFailure(t, got, 3, "constraint_failed")
	if !strings.Contains(got.Stderr, "sample HG003_father: ") {
		t.Errorf("stderr = %q, want the sample", got.Stderr)
	}
	if n := count(t, path, "fastp_qc"); n != "0" {
		t.Errorf("%s samples written", n)
	}
	if n := count(t, path, "fastp_qc_history"); n != "0" {
		t.Errorf("%s history rows written", n)
	}
	if runs := query(t, path, "SELECT exit_code FROM tool_runs"); len(runs) != 1 || runs[0][0] != "3" {
		t.Errorf("tool_runs = %v, want one run with exit 3", runs)
	}
}

func TestIngestNeedsAUniqueSample(t *testing.T) {
	t.Parallel()
	path := newDB(t, strings.Replace(golden(t, fastpGolden), `  UNIQUE ("sample")`, `  "other" TEXT`, 1))

	requireFailure(t, ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp"), 2, "no_unique_key")
}

func TestQueryReadsIngestedColumnsWithADot(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))
	requireSuccess(t, ingest(t, path, "qc", multiqcFixture))

	got := runScribo(t, "query", "--db", path, "--table", "qc", "--where", "fastp.pct_duplication>4", "--fields", "sample,fastp.pct_duplication")

	if got.ExitCode != 0 || got.Stdout != "sample\tfastp.pct_duplication\nHG004_mother\t4.09184\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", got.ExitCode, got.Stdout, got.Stderr)
	}
}

func TestIngestRefusesAMissingOrInvalidFile(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, generalGolden))
	invalid := writeTemp(t, "multiqc_data.json", "{")

	requireFailure(t, ingest(t, path, "qc", path+".missing.json"), 4, "file_not_found")
	requireFailure(t, ingest(t, path, "qc", invalid), 4, "invalid_format")
	if n := scalar(t, path, "SELECT count(*) FROM sqlite_schema WHERE name = 'tool_runs'"); n != "0" {
		t.Error("a refused file began a run")
	}
}

func TestIngestAggregatesTheReconciliationWarning(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, fastpGolden))
	requireSuccess(t, ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp"))
	mustExec(t, path, "UPDATE fastp_qc SET pct_duplication = 99 WHERE sample IN ('HG001', 'HG004_mother')")
	mustExec(t, path, "UPDATE fastp_qc SET pct_adapter = 1 WHERE sample = 'HG004_mother'")

	got := ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp")

	requireWarnings(t, got, "history_reconciled")
	if !strings.Contains(got.Stderr, "fastp_qc: 2 records reconciled: HG001, HG004_mother\n") {
		t.Errorf("stderr = %q", got.Stderr)
	}
	for _, field := range []string{"pct_duplication", "pct_adapter"} {
		rows := query(t, path, `SELECT record_key, ifnull(old_value, 'NULL'), old_is_null, ifnull(new_value, 'NULL'), new_is_null
			FROM fastp_qc_history WHERE field = ? ORDER BY record_key, changed_at, id`, field)
		for i, row := range rows {
			if i == 0 || rows[i-1][0] != row[0] {
				if row[2] != "1" {
					t.Errorf("%s: first row %v has a non-NULL old value", field, row)
				}
				continue
			}
			if previous := rows[i-1]; row[1] != previous[3] || row[2] != previous[4] {
				t.Errorf("%s: row %v does not continue %v", field, row, previous)
			}
		}
	}
	if n := scalar(t, path, "SELECT count(*) FROM fastp_qc_history WHERE source = 'reconciled'"); n != "3" {
		t.Errorf("reconciled rows = %s, want 3", n)
	}
}

func TestIngestRefusesOneMissingColumnByName(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, fastpGolden), `ALTER TABLE fastp_qc DROP COLUMN pct_adapter`)

	got := ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp")

	requireFailure(t, got, 2, "column_not_found")
	if !strings.Contains(got.Stderr, "column pct_adapter does not exist in fastp_qc") {
		t.Errorf("stderr = %q", got.Stderr)
	}
	if n := count(t, path, "fastp_qc"); n != "0" {
		t.Errorf("fastp_qc has %s rows, want 0", n)
	}
}

func TestIngestNamesTheOneRecordReconciledAmongMany(t *testing.T) {
	t.Parallel()
	path := newDB(t, golden(t, fastpGolden))
	requireSuccess(t, ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp"))
	mustExec(t, path, "UPDATE fastp_qc SET pct_duplication = 99 WHERE sample = 'HG001'")

	got := ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp")

	requireWarnings(t, got, "history_reconciled")
	if !strings.Contains(got.Stderr, "fastp_qc: 1 record reconciled: HG001\n") {
		t.Errorf("stderr = %q", got.Stderr)
	}
}

func TestIngestModuleWithoutAVersion(t *testing.T) {
	t.Parallel()
	path := newDB(t, schemaFor(t, "star_qc", "--module", "star"))

	got := ingest(t, path, "star_qc", multiqcFixture, "--module", "star")

	requireWarnings(t, got, "tool_version_unknown")
	if rows := query(t, path, "SELECT count(*), count(tool_version) FROM star_qc"); rows[0][0] != "2" || rows[0][1] != "0" {
		t.Errorf("rows, versions = %v, want 2 rows and every tool_version NULL", rows)
	}
}

func TestIngestWarnsAboutSkippedMetrics(t *testing.T) {
	t.Parallel()
	report := writeTemp(t, "multiqc_data.json", `{
	  "report_general_stats_headers": {"tool": {"kept": {"namespace": "tool"}}},
	  "report_general_stats_data": {"tool": {"S1": {"kept": 1, "tool_version": "2.0", "curve": [1, 2]}}}
	}`)
	ddl := runScribo(t, "schema", "--from-multiqc", report, "--table", "tool_qc", "--module", "tool")
	path := newDB(t, ddl.Stdout)

	got := ingest(t, path, "tool_qc", report, "--module", "tool")

	requireWarnings(t, got, "metrics_skipped")
	if !strings.Contains(got.Stderr, "tool_qc: 1 metrics left out: curve") {
		t.Errorf("stderr = %q", got.Stderr)
	}
	if rows := query(t, path, "SELECT sample, kept, tool_version FROM tool_qc"); fmtRows(rows) != "[[S1 1 2.0]]" {
		t.Errorf("rows = %v", rows)
	}
}

func TestIngestLeavesOtherSamplesAndColumnsAlone(t *testing.T) {
	t.Parallel()
	ddl := strings.Replace(golden(t, fastpGolden), `  UNIQUE ("sample")`, `  "note" TEXT,`+"\n"+`  UNIQUE ("sample")`, 1)
	path := newDB(t, ddl, `INSERT INTO fastp_qc (sample, note) VALUES ('OTHER', 'kept')`)
	mustExec(t, path, `INSERT INTO fastp_qc (sample, note, tool_version) VALUES ('HG001', 'mine', 'old')`)

	requireSuccess(t, ingest(t, path, "fastp_qc", multiqcFixture, "--module", "fastp"))

	rows := query(t, path, "SELECT sample, ifnull(note, 'NULL'), ifnull(tool_version, 'NULL') FROM fastp_qc WHERE sample IN ('OTHER', 'HG001') ORDER BY sample")
	if fmtRows(rows) != "[[HG001 mine 0.23.4] [OTHER kept NULL]]" {
		t.Errorf("rows = %v", rows)
	}
}
