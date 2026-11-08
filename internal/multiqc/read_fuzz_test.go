package multiqc

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func requireTypedError(t *testing.T, err error) {
	t.Helper()
	var domain *exitcode.Error
	if err != nil && !errors.As(err, &domain) {
		t.Fatalf("error %v is not an *exitcode.Error", err)
	}
}

func FuzzMultiQCRead(f *testing.F) {
	fixture, err := os.ReadFile("../../testdata/multiqc/multiqc_data.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(fixture)
	for _, seed := range []string{
		``, `{`, `[]`, `{"report_general_stats_data": {}}`, `{"report_general_stats_data": {"g": 3}}`,
		`{"report_general_stats_headers": {"g": {"m": {"namespace": "T"}}}, "report_general_stats_data": {"g": {"S1": {"m": 1, "t_version": "1"}}}}`,
		`{"report_general_stats_data": {"tool": {"S1": {"m": 1}}}, "report_saved_raw_data": {"multiqc_tool": {"S1": {"tool_version": "2"}}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, document []byte) {
		report, err := Read(bytes.NewReader(document))
		requireTypedError(t, err)
		if err != nil {
			return
		}
		_, err = report.GeneralStats()
		requireTypedError(t, err)
		for _, module := range report.Modules() {
			_, err := report.Module(module)
			requireTypedError(t, err)
		}
	})
}
