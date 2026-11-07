package exitcode

import (
	"os"
	"regexp"
	"testing"
)

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

func TestWarningFormat(t *testing.T) {
	tests := []struct {
		name    string
		warning Warning
		want    string
	}{
		{"with context", Warning{"network_filesystem", "journal mode DELETE", "spool"}, "WARNING [network_filesystem]: journal mode DELETE\n→ spool\n"},
		{"without context", Warning{"network_filesystem", "journal mode DELETE", ""}, "WARNING [network_filesystem]: journal mode DELETE\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.warning.Format(); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func specTypedCodes(t *testing.T) []string {
	t.Helper()
	spec, err := os.ReadFile("../../docs/SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile("(?m)^Typed codes:.*$").FindString(string(spec))
	if line == "" {
		t.Fatal("docs/SPEC.md has no line starting with \"Typed codes:\"")
	}
	var codes []string
	for _, match := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(line, -1) {
		codes = append(codes, match[1])
	}
	return codes
}

func TestCodesRegistryMatchesTheSpec(t *testing.T) {
	inSpec := map[string]bool{}
	for _, code := range specTypedCodes(t) {
		inSpec[code] = true
	}
	inRegistry := map[string]bool{}
	for _, info := range Codes {
		if inRegistry[info.Code] {
			t.Errorf("code %s is in the registry twice", info.Code)
		}
		inRegistry[info.Code] = true
		if info.Meaning == "" {
			t.Errorf("code %s has no meaning", info.Code)
		}
		if info.Status < WarningStatus || info.Status > System {
			t.Errorf("code %s has status %d", info.Code, info.Status)
		}
	}
	for code := range inSpec {
		if !inRegistry[code] {
			t.Errorf("code %s is in SPEC §12 but not in exitcode.Codes", code)
		}
	}
	for code := range inRegistry {
		if !inSpec[code] {
			t.Errorf("code %s is in exitcode.Codes but not in SPEC §12", code)
		}
	}
	if len(inSpec) == 0 {
		t.Error("no code found in SPEC §12")
	}
}
