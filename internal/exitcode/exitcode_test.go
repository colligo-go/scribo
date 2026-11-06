package exitcode

import "testing"

func TestFormat(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "with context",
			err:  New(Data, CodeInvalidValue, "Alignment_Rate is not a number", "line 3"),
			want: "ERROR [invalid_value]: Alignment_Rate is not a number\n→ line 3\n",
		},
		{
			name: "without context",
			err:  New(Usage, CodeUsage, "missing --db", ""),
			want: "ERROR [usage]: missing --db\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Format(); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusesAreDistinct(t *testing.T) {
	statuses := []int{OK, Usage, Database, Data, File, System}
	seen := map[int]bool{}
	for i, status := range statuses {
		if status != i {
			t.Errorf("status %d at position %d, want the SPEC §12 numbering", status, i)
		}
		if seen[status] {
			t.Errorf("status %d repeated", status)
		}
		seen[status] = true
	}
}
