package writer

import (
	"strings"
	"testing"
)

func TestSuggestion(t *testing.T) {
	columns := []string{"sample", "Alignment_Rate", "Mapped_Reads", "alignment_rate2"}
	tests := []struct {
		name string
		want string
	}{
		{"Alignment_rate", `did you mean "Alignment_Rate"?`},
		{"alignment_rat", `did you mean "Alignment_Rate"?`},
		{"sampel", `did you mean "sample"?`},
		{"Mapped_Read", `did you mean "Mapped_Reads"?`},
		{"zzzzzzzz", "columns: Alignment_Rate, Mapped_Reads, alignment_rate2, sample"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := suggestion(tt.name, columns); got != tt.want {
				t.Errorf("suggestion(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestSuggestionTieGoesToTheAlphabeticallyFirst(t *testing.T) {
	if got := suggestion("ab", []string{"ac", "aa"}); got != `did you mean "aa"?` {
		t.Errorf("got %q", got)
	}
}

func TestSuggestionListsAtMostTwentyColumns(t *testing.T) {
	var columns []string
	for _, letter := range "abcdefghijklmnopqrstuvwxyz" {
		columns = append(columns, strings.Repeat(string(letter), 9))
	}
	got := suggestion("0000000000", columns)
	if listed := strings.Count(got, ",") + 1; listed != 20 {
		t.Errorf("listed %d columns, want 20: %s", listed, got)
	}
}

func TestEditDistanceCountsRunesNotBytes(t *testing.T) {
	if got := editDistance("café", "cafe"); got != 1 {
		t.Errorf("editDistance = %d, want 1", got)
	}
}

func TestReservedTables(t *testing.T) {
	for _, name := range []string{"tool_runs", "TOOL_RUNS", "samples", "files", "facts", "result_inputs", "input_signals", "study_meta", "x_history", "X_HISTORY", "sqlite_master", "SQLITE_sequence"} {
		if !isReserved(name) {
			t.Errorf("%s should be reserved", name)
		}
	}
	for _, name := range []string{"rnaseq_qc", "history", "my_sqlite", "samples2"} {
		if isReserved(name) {
			t.Errorf("%s should not be reserved", name)
		}
	}
}
