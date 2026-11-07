package validate

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
			if got := Suggestion(tt.name, columns); got != tt.want {
				t.Errorf("suggestion(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestSuggestionTieGoesToTheAlphabeticallyFirst(t *testing.T) {
	if got := Suggestion("ab", []string{"ac", "aa"}); got != `did you mean "aa"?` {
		t.Errorf("got %q", got)
	}
}

func TestSuggestionListsAtMostTwentyColumns(t *testing.T) {
	var columns []string
	for _, letter := range "abcdefghijklmnopqrstuvwxyz" {
		columns = append(columns, strings.Repeat(string(letter), 9))
	}
	got := Suggestion("0000000000", columns)
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
		if !Reserved(name) {
			t.Errorf("%s should be reserved", name)
		}
	}
	for _, name := range []string{"rnaseq_qc", "history", "my_sqlite", "samples2"} {
		if Reserved(name) {
			t.Errorf("%s should not be reserved", name)
		}
	}
}

func TestSuggestionByPrefix(t *testing.T) {
	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{"rnaseq", []string{"rnaseq_qc", "star_qc"}, `did you mean "rnaseq_qc"?`},
		{"RNASEQ", []string{"rnaseq_qc"}, `did you mean "rnaseq_qc"?`},
		{"rnaseq", []string{"rnaseq_qc", "rnaseq_counts"}, "columns: rnaseq_counts, rnaseq_qc"},
		{"Align", []string{"Alignment_Rate", "Mapped_Reads"}, `did you mean "Alignment_Rate"?`},
		{"rnaseqqc", []string{"rnaseq_qc", "rnaseq_q"}, `did you mean "rnaseq_qc"?`},
		{"zzz", []string{"a", "b"}, "columns: a, b"},
		{"", []string{"abcdef"}, "columns: abcdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" in "+strings.Join(tt.candidates, ","), func(t *testing.T) {
			if got := Suggestion(tt.name, tt.candidates); got != tt.want {
				t.Errorf("Suggestion(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestTableSuggestionByPrefix(t *testing.T) {
	if got := TableSuggestion("rnaseq", []string{"rnaseq_qc", "star_qc"}); got != `did you mean "rnaseq_qc"?` {
		t.Errorf("got %q", got)
	}
}
