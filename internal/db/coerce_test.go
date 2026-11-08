package db

import (
	"errors"
	"reflect"
	"testing"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func column(declared string) Column {
	return Column{Name: "f", DeclaredType: declared, Affinity: AffinityOf(declared), Declared: declared != "", Nullable: true}
}

func TestCoerce(t *testing.T) {
	tests := []struct {
		name    string
		declare string
		raw     string
		want    any
		wantErr bool
	}{
		{"integer", "INTEGER", "12", int64(12), false},
		{"negative integer", "INTEGER", "-3", int64(-3), false},
		{"leading zero", "INTEGER", "01", int64(1), false},
		{"integer overflow", "INTEGER", "9223372036854775808", nil, true},
		{"integer max", "INTEGER", "9223372036854775807", int64(9223372036854775807), false},
		{"decimal into integer", "INTEGER", "94.3", nil, true},
		{"exponent into integer", "INTEGER", "1e3", nil, true},
		{"empty into integer", "INTEGER", "", nil, true},

		{"real", "REAL", "94.3", 94.3, false},
		{"integer into real", "REAL", "5", int64(5), false},
		{"leading dot", "REAL", ".5", 0.5, false},
		{"trailing dot", "REAL", "5.", 5.0, false},
		{"exponent", "REAL", "1e3", 1000.0, false},
		{"signed exponent", "REAL", "-2.5E-1", -0.25, false},
		{"big integer into real", "REAL", "9223372036854775808", 9223372036854775808.0, false},
		{"rendered 1e19 into real", "REAL", "10000000000000000000", 1e19, false},
		{"rendered 1e19 into integer", "INTEGER", "10000000000000000000", nil, true},
		{"rendered 1e19 into numeric", "NUMERIC", "10000000000000000000", 1e19, false},
		{"NaN", "REAL", "NaN", nil, true},
		{"Inf", "REAL", "Inf", nil, true},
		{"infinity", "REAL", "infinity", nil, true},
		{"hex", "REAL", "0x1A", nil, true},
		{"hex float", "REAL", "0x1p-2", nil, true},
		{"leading space", "REAL", " 5", nil, true},
		{"trailing space", "REAL", "5 ", nil, true},
		{"underscore", "REAL", "1_000", nil, true},
		{"word", "REAL", "abc", nil, true},
		{"empty into real", "REAL", "", nil, true},
		{"range overflow", "REAL", "1e999", nil, true},
		{"lone sign", "REAL", "-", nil, true},
		{"lone dot", "REAL", ".", nil, true},

		{"decimal", "DECIMAL(10,2)", "3.0", 3.0, false},
		{"decimal word", "DECIMAL(10,2)", "abc", nil, true},
		{"numeric", "NUMERIC", "7", int64(7), false},

		{"datetime", "DATETIME", "2026-11-06 12:00:00", "2026-11-06 12:00:00", false},
		{"datetime word", "DATETIME", "abc", "abc", false},
		{"boolean", "BOOLEAN", "yes", "yes", false},
		{"text", "TEXT", "01", "01", false},
		{"text empty", "TEXT", "", "", false},
		{"untyped", "", "abc", "abc", false},
		{"blob", "BLOB", "x", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Coerce(column(tt.declare), Value{Raw: tt.raw})
			if tt.wantErr {
				var domain *exitcode.Error
				if !errors.As(err, &domain) || domain.Code != exitcode.CodeInvalidValue || domain.Status != exitcode.Data {
					t.Fatalf("Coerce(%q) = %v, %v, want an invalid_value error", tt.raw, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Coerce(%q): %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Coerce(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCoerceNull(t *testing.T) {
	got, err := Coerce(column("INTEGER"), Value{IsNull: true, Raw: "ignored"})
	if err != nil || got != nil {
		t.Errorf("Coerce(NULL) = %v, %v, want nil, nil", got, err)
	}
}

func TestCoerceErrorMessage(t *testing.T) {
	c := column("REAL")
	c.Name = "Alignment_Rate"
	_, err := Coerce(c, Value{Raw: "abc"})

	want := `Alignment_Rate is REAL and "abc" is not a number`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestCoerceHints(t *testing.T) {
	tests := []struct {
		name    string
		declare string
		raw     string
		want    string
	}{
		{"percent into real", "REAL", "94.3%", "drop the % sign: 94.3"},
		{"percent into integer", "INTEGER", "12%", "drop the % sign: 12"},
		{"decimal percent into integer", "INTEGER", "94.3%", "drop the % sign"},
		{"signed percent", "REAL", "-5%", "drop the % sign: -5"},
		{"thousands by commas", "REAL", "12,000,000", "drop the thousands separators: 12000000"},
		{"thousands by spaces", "REAL", "12 000 000", "drop the thousands separators: 12000000"},
		{"thousands with decimals", "REAL", "1,000,000.5", "drop the thousands separators: 1000000.5"},
		{"thousands with decimals into integer", "INTEGER", "1,000,000.5", "drop the thousands separators"},
		{"thousands into integer", "INTEGER", "12,000,000", "drop the thousands separators: 12000000"},
		{"ambiguous", "REAL", "1,234", "use a dot as the decimal separator (1.234) or drop the thousands separator (1234)"},
		{"signed ambiguous", "REAL", "-1,234", "use a dot as the decimal separator (-1.234) or drop the thousands separator (-1234)"},
		{"comma as decimal point", "REAL", "94,3", "use a dot as the decimal separator: 94.3"},
		{"negative comma as decimal point", "REAL", "-0,5", "use a dot as the decimal separator: -0.5"},
		{"comma into integer has no hint", "INTEGER", "94,3", ""},
		{"word", "REAL", "abc", ""},
		{"percent of a word", "REAL", "abc%", ""},
		{"misplaced groups", "REAL", "1,23,456", ""},
		{"two decimal points", "REAL", "1.2.3", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Coerce(column(tt.declare), Value{Raw: tt.raw})

			var domain *exitcode.Error
			if !errors.As(err, &domain) {
				t.Fatalf("Coerce(%q) = %v, want an error", tt.raw, err)
			}
			if domain.Context != tt.want {
				t.Errorf("context = %q, want %q", domain.Context, tt.want)
			}
		})
	}
}

func TestHintsNeverChangeWhatIsAccepted(t *testing.T) {
	for _, raw := range []string{"94.3", "12000000", "1234", "-0.5", "1e3"} {
		if _, err := Coerce(column("REAL"), Value{Raw: raw}); err != nil {
			t.Errorf("Coerce(%q) = %v, want it accepted", raw, err)
		}
	}
}
