package multiqc

import (
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/colligo-go/scribo/internal/exitcode"
)

var readFixture = sync.OnceValues(func() (*Report, error) {
	file, err := os.Open("../../testdata/multiqc/multiqc_data.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Read(file)
})

func fixture(t *testing.T) *Report {
	t.Helper()
	report, err := readFixture()
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func synthetic(t *testing.T, document string) *Report {
	t.Helper()
	report, err := Read(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func requireCode(t *testing.T, err error, status int, code string, mentions ...string) {
	t.Helper()
	var domain *exitcode.Error
	if !errors.As(err, &domain) || domain.Status != status || domain.Code != code {
		t.Fatalf("error = %v, want %s (exit %d)", err, code, status)
	}
	text := domain.Message + " " + domain.Context
	for _, mention := range mentions {
		if !strings.Contains(text, mention) {
			t.Errorf("error %q does not mention %q", text, mention)
		}
	}
}

func columnNames(table Table) []string {
	names := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		names[i] = column.Name
	}
	return names
}

func typeOf(t *testing.T, table Table, name string) string {
	t.Helper()
	for _, column := range table.Columns {
		if column.Name == name {
			return column.Type
		}
	}
	t.Fatalf("no column %q in %v", name, columnNames(table))
	return ""
}

func TestModulesOfTheFixture(t *testing.T) {
	got := fixture(t).Modules()

	if want := []string{"cutadapt", "fastp", "fastqc", "salmon", "star"}; !slices.Equal(got, want) {
		t.Errorf("Modules() = %v, want %v", got, want)
	}
}

func TestGeneralStatsOfTheFixture(t *testing.T) {
	table, err := fixture(t).GeneralStats()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"cutadapt.percent_trimmed",
		"fastp.after_filtering_gc_content", "fastp.after_filtering_q30_bases", "fastp.after_filtering_q30_rate",
		"fastp.filtering_result_passed_filter_reads", "fastp.pct_adapter", "fastp.pct_duplication", "fastp.pct_surviving",
		"fastqc.avg_sequence_length", "fastqc.median_sequence_length", "fastqc.percent_duplicates", "fastqc.percent_fails",
		"fastqc.percent_gc", "fastqc.total_sequences",
		"salmon.compatible_fragment_ratio", "salmon.library_types", "salmon.num_mapped", "salmon.percent_mapped",
		"star.mapped", "star.mapped_percent", "star.multimapped", "star.total_reads", "star.uniquely_mapped",
		"star.uniquely_mapped_percent",
	}
	if got := columnNames(table); !slices.Equal(got, want) {
		t.Errorf("columns = %v\nwant %v", got, want)
	}
	if slices.Contains(columnNames(table), "salmon.strand_mapping_bias") {
		t.Error("salmon_3 lists strand_mapping_bias in its headers but no sample has it; it must not become a column")
	}
	if got := table.Rows["SRR1067503_1"]["star.total_reads"]; got != "30533631.0" {
		t.Errorf("star.total_reads = %q, want the raw 30533631.0, not the scaled value", got)
	}
	if got := typeOf(t, table, "star.total_reads"); got != TypeReal {
		t.Errorf("star.total_reads is %s, want REAL", got)
	}
	if got := table.Samples(); len(got) != 9 {
		t.Errorf("samples = %v, want 9", got)
	}
	if table.HasVersion || len(table.Skipped) != 0 {
		t.Errorf("HasVersion %v, Skipped %v", table.HasVersion, table.Skipped)
	}
}

func TestModuleFastp(t *testing.T) {
	table, err := fixture(t).Module("fastp")
	if err != nil {
		t.Fatal(err)
	}

	if got := table.Samples(); !slices.Equal(got, []string{"HG001", "HG002_son", "HG003_father", "HG004_mother"}) {
		t.Errorf("samples = %v", got)
	}
	names := columnNames(table)
	for _, want := range []string{"before_filtering_total_reads", "after_filtering_total_reads"} {
		if !slices.Contains(names, want) {
			t.Errorf("no column %s in %v", want, names)
		}
	}
	for _, name := range names {
		if strings.Contains(name, "kmer") || strings.Contains(name, "adapter_counts") || strings.HasPrefix(name, "fastp.") {
			t.Errorf("column %s comes from the raw data or carries a prefix", name)
		}
	}
	if len(names) != 20 {
		t.Errorf("%d columns, want the 20 general fields of fastp: %v", len(names), names)
	}
	if got := table.Versions["HG001"]; got != "0.23.4" || !table.HasVersion {
		t.Errorf("version of HG001 = %q, HasVersion %v, want 0.23.4 from summary.fastp_version", got, table.HasVersion)
	}
	if got := table.Rows["HG001"]["after_filtering_total_reads"]; got != "593374960.0" {
		t.Errorf("after_filtering_total_reads = %q", got)
	}
}

func TestModuleCutadaptTakesTheVersionFromTheGeneralData(t *testing.T) {
	table, err := fixture(t).Module("cutadapt")
	if err != nil {
		t.Fatal(err)
	}

	if table.Versions["SRR1067503_1"] != "1.8" || table.Versions["SRR1067505_1"] != "1.8" || !table.HasVersion {
		t.Errorf("versions = %v", table.Versions)
	}
	if slices.Contains(columnNames(table), "cutadapt_version") {
		t.Error("cutadapt_version is a column")
	}
	if got := typeOf(t, table, "bp_processed"); got != TypeInteger {
		t.Errorf("bp_processed is %s, want INTEGER", got)
	}
}

func TestModuleSalmonMergesItsGroups(t *testing.T) {
	table, err := fixture(t).Module("Salmon")
	if err != nil {
		t.Fatal(err)
	}

	if got := table.Samples(); !slices.Equal(got, []string{"DRR016125_quant"}) {
		t.Fatalf("samples = %v", got)
	}
	row := table.Rows["DRR016125_quant"]
	if row["num_mapped"] != "11674255" || row["num_compatible_fragments"] != "11452777" {
		t.Errorf("row = %v, want fields of both salmon and salmon_3", row)
	}
	if table.Versions["DRR016125_quant"] != "0.9.1" {
		t.Errorf("versions = %v", table.Versions)
	}
	if got := typeOf(t, table, "seq_bias_correct"); got != TypeText || row["seq_bias_correct"] != "true" {
		t.Errorf("seq_bias_correct is %s %q, want TEXT \"true\"", got, row["seq_bias_correct"])
	}
}

func TestModulesWithoutAVersion(t *testing.T) {
	report := fixture(t)
	for _, module := range []string{"star", "fastqc"} {
		table, err := report.Module(module)
		if err != nil {
			t.Fatal(err)
		}
		if table.HasVersion || len(table.Versions) != 0 {
			t.Errorf("%s: HasVersion %v, versions %v", module, table.HasVersion, table.Versions)
		}
	}
}

const twoSamples = `{
  "report_general_stats_headers": {"tool": {"m": {"namespace": "Tool"}}},
  "report_general_stats_data": {"tool": {"S1": {"m": %s}, "S2": {"m": %s}}}
}`

func TestColumnTypeFollowsTheJSONText(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		want   string
	}{
		{"integral float and integer", "593374960.0", "12", TypeReal},
		{"integers", "12", "13", TypeInteger},
		{"exponent", "12", "1e3", TypeReal},
		{"text", "1", `"n/a"`, TypeText},
		{"bool", "1", "true", TypeText},
		{"null", "null", "null", TypeText},
		{"integer and null", "7", "null", TypeInteger},
		{"overflowing integer", "12", "10000000000000000000", TypeReal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := synthetic(t, strings.Replace(strings.Replace(twoSamples, "%s", tt.first, 1), "%s", tt.second, 1))
			for _, read := range []func() (Table, error){report.GeneralStats, func() (Table, error) { return report.Module("tool") }} {
				table, err := read()
				if err != nil {
					t.Fatal(err)
				}
				if len(table.Columns) != 1 || table.Columns[0].Type != tt.want {
					t.Errorf("columns = %v, want one %s", table.Columns, tt.want)
				}
			}
		})
	}
}

func TestColumnTypeWithThreeValues(t *testing.T) {
	report := synthetic(t, `{
	  "report_general_stats_data": {"tool": {"S1": {"m": 1}, "S2": {"m": 2}, "S3": {"m": "n/a"}}}
	}`)
	table, err := report.Module("tool")
	if err != nil {
		t.Fatal(err)
	}
	if got := typeOf(t, table, "m"); got != TypeText {
		t.Errorf("m is %s, want TEXT", got)
	}
}

func TestGroupsOfOneModuleThatDisagree(t *testing.T) {
	report := synthetic(t, `{
	  "report_general_stats_headers": {"salmon": {"a": {"namespace": "Salmon"}}, "salmon_3": {"b": {"namespace": "Salmon"}}},
	  "report_general_stats_data": {
	    "salmon": {"DRR016125_quant": {"num_compatible_fragments": 10}},
	    "salmon_3": {"DRR016125_quant": {"num_compatible_fragments": 11}}
	  }
	}`)

	_, err := report.Module("salmon")

	requireCode(t, err, exitcode.File, exitcode.CodeInvalidFormat, "salmon and salmon_3 disagree on num_compatible_fragments for DRR016125_quant")
}

func TestGroupsOfOneModuleThatAgree(t *testing.T) {
	report := synthetic(t, `{
	  "report_general_stats_headers": {"a": {"x": {"namespace": "T"}}, "b": {"y": {"namespace": "T"}}},
	  "report_general_stats_data": {"a": {"S1": {"same": 1, "x": 2}}, "b": {"S1": {"same": 1, "y": 3}}}
	}`)

	table, err := report.Module("t")
	if err != nil {
		t.Fatal(err)
	}
	if got := columnNames(table); !slices.Equal(got, []string{"same", "x", "y"}) {
		t.Errorf("columns = %v", got)
	}
}

func TestObjectsAndListsAreSkipped(t *testing.T) {
	report := synthetic(t, `{
	  "report_general_stats_headers": {"tool": {"nested": {"namespace": "tool"}, "listed": {}, "kept": {}}},
	  "report_general_stats_data": {"tool": {
	    "S1": {"nested": {"a": 1}, "listed": [1, 2], "kept": 3},
	    "S2": {"nested": 4, "kept": 5}
	  }}
	}`)

	module, err := report.Module("tool")
	if err != nil {
		t.Fatal(err)
	}
	if got := columnNames(module); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("columns = %v", got)
	}
	if !slices.Equal(module.Skipped, []string{"listed", "nested"}) {
		t.Errorf("Skipped = %v", module.Skipped)
	}
	if _, ok := module.Rows["S2"]["nested"]; ok {
		t.Error("a skipped field kept the scalar value of another sample")
	}

	general, err := report.GeneralStats()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(general.Skipped, []string{"tool.listed", "tool.nested"}) {
		t.Errorf("Skipped = %v", general.Skipped)
	}
}

func TestUnknownModule(t *testing.T) {
	_, err := fixture(t).Module("nope")

	requireCode(t, err, exitcode.File, exitcode.CodeModuleNotFound, "nope", "modules: cutadapt, fastp, fastqc, salmon, star")
}

func TestInvalidDocuments(t *testing.T) {
	content, err := os.ReadFile("../../testdata/multiqc/multiqc_data.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		document string
		mention  string
	}{
		{"truncated", string(content[:len(content)/2]), "ends early"},
		{"syntax", `{"report_general_stats_data": {,}}`, "byte 32"},
		{"not an object", `[1, 2]`, "not a JSON object"},
		{"empty", ``, "ends early"},
		{"trailing data", `{"report_general_stats_data": {}} {}`, "after the JSON object"},
		{"no general stats", `{"report_saved_raw_data": {}}`, "report_general_stats_data"},
		{"general stats is a list", `{"report_general_stats_data": [{}]}`, "report_general_stats_data"},
		{"sample is not an object", `{"report_general_stats_data": {"g": {"S1": 3}}}`, "report_general_stats_data.g.S1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(strings.NewReader(tt.document))

			requireCode(t, err, exitcode.File, exitcode.CodeInvalidFormat, tt.mention)
		})
	}
}

func TestVersionSearchIsDepthFirstInByteOrder(t *testing.T) {
	report := synthetic(t, `{
	  "report_general_stats_data": {"tool": {"S1": {"m": 1}, "S2": {"m": 2}, "S3": {"m": 3}}},
	  "report_saved_raw_data": {"multiqc_tool": {
	    "S1": {"b": {"tool_version": "2"}, "a": {"deep": {"tool_version": "1"}}},
	    "S2": {"tool_version": 9, "z": {"tool_version": "text wins"}},
	    "S3": {"other": "x"}
	  }}
	}`)

	table, err := report.Module("tool")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"S1": "1", "S2": "text wins"}
	if len(table.Versions) != len(want) || table.Versions["S1"] != "1" || table.Versions["S2"] != "text wins" {
		t.Errorf("versions = %v, want %v", table.Versions, want)
	}
}
