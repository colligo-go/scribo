package validate

import (
	"errors"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func TestParseWhere(t *testing.T) {
	tests := []struct {
		text      string
		wantField string
		wantOp    db.Op
		wantValue string
		wantErr   bool
	}{
		{"a=1", "a", db.OpEq, "1", false},
		{"a!=1", "a", db.OpNe, "1", false},
		{"a>1", "a", db.OpGt, "1", false},
		{"a<1", "a", db.OpLt, "1", false},
		{"a>=1", "a", db.OpGe, "1", false},
		{"a<=1", "a", db.OpLe, "1", false},
		{"note=a=b", "note", db.OpEq, "a=b", false},
		{"note=>x", "note", db.OpEq, ">x", false},
		{"note>=<", "note", db.OpGe, "<", false},
		{"a=", "a", db.OpEq, "", false},
		{"a!b=c", "a!b", db.OpEq, "c", false},
		{"a=null", "a", db.OpEq, "null", false},
		{"=1", "", 0, "", true},
		{">=1", "", 0, "", true},
		{"a", "", 0, "", true},
		{"a!1", "", 0, "", true},
		{"", "", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			field, op, value, err := ParseWhere(tt.text)
			if tt.wantErr {
				var domain *exitcode.Error
				if !errors.As(err, &domain) || domain.Code != exitcode.CodeUsage || domain.Status != exitcode.Usage {
					t.Fatalf("ParseWhere(%q) = %v, want a usage error", tt.text, err)
				}
				return
			}
			if err != nil || field != tt.wantField || op != tt.wantOp || value != tt.wantValue {
				t.Errorf("ParseWhere(%q) = %q %v %q %v, want %q %v %q", tt.text, field, op, value, err, tt.wantField, tt.wantOp, tt.wantValue)
			}
		})
	}
}

func TestOpString(t *testing.T) {
	want := map[db.Op]string{db.OpEq: "=", db.OpNe: "!=", db.OpLt: "<", db.OpLe: "<=", db.OpGt: ">", db.OpGe: ">=", db.OpIsNull: "IS NULL", db.OpNotNull: "IS NOT NULL"}
	for op, text := range want {
		if op.String() != text {
			t.Errorf("%d.String() = %q, want %q", op, op.String(), text)
		}
	}
}

func TestPredicates(t *testing.T) {
	columns := map[string]db.Column{
		"rate":     {Name: "rate", DeclaredType: "REAL", Affinity: "REAL", Declared: true},
		"run_date": {Name: "run_date", DeclaredType: "DATETIME", Affinity: "NUMERIC", Declared: true},
		"note":     {Name: "note", DeclaredType: "TEXT", Affinity: "TEXT", Declared: true},
		"untyped":  {Name: "untyped", Affinity: "BLOB"},
	}
	tests := []struct {
		name     string
		filters  Filters
		wantCode string
	}{
		{"number on a numeric column", Filters{Where: []string{"rate>90"}}, ""},
		{"word on a numeric column", Filters{Where: []string{"rate>abc"}}, exitcode.CodeInvalidValue},
		{"equal on text", Filters{Where: []string{"note=x"}}, ""},
		{"ordering on text", Filters{Where: []string{"note<x"}}, exitcode.CodeUntypedComparison},
		{"ordering on a date", Filters{Where: []string{"run_date>=2026"}}, exitcode.CodeUntypedComparison},
		{"ordering on an untyped column", Filters{Where: []string{"untyped>1"}}, exitcode.CodeUntypedComparison},
		{"ordering forced to text", Filters{Where: []string{"run_date>=2026"}, ForceText: true}, ""},
		{"word forced to text on a number", Filters{Where: []string{"rate>abc"}, ForceText: true}, ""},
		{"null tests", Filters{IsNull: []string{"note"}, NotNull: []string{"rate"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Predicates(columns, tt.filters)
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Predicates = %v, want nil", err)
				}
				return
			}
			var domain *exitcode.Error
			if !errors.As(err, &domain) || domain.Code != tt.wantCode {
				t.Errorf("Predicates = %v, want %s", err, tt.wantCode)
			}
		})
	}
}

func TestPredicatesKeepTheTextNull(t *testing.T) {
	columns := map[string]db.Column{"note": {Name: "note", DeclaredType: "TEXT", Affinity: "TEXT", Declared: true}}
	predicates, err := Predicates(columns, Filters{Where: []string{"note=null"}})
	if err != nil || len(predicates) != 1 {
		t.Fatalf("Predicates = %v, %v", predicates, err)
	}
	if got := predicates[0]; got.Op != db.OpEq || got.Value != (db.Value{Raw: "null"}) {
		t.Errorf("predicate = %+v, want = with the text null", got)
	}
}
