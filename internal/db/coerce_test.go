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
