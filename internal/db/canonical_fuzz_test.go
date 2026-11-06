package db

import "testing"

var fuzzColumns = []Column{
	{Name: "i", DeclaredType: "INTEGER", Affinity: "INTEGER", Declared: true},
	{Name: "r", DeclaredType: "REAL", Affinity: "REAL", Declared: true},
	{Name: "n", DeclaredType: "NUMERIC", Affinity: "NUMERIC", Declared: true},
	{Name: "d", DeclaredType: "DECIMAL(10,2)", Affinity: "NUMERIC", Declared: true},
	{Name: "t", DeclaredType: "TEXT", Affinity: "TEXT", Declared: true},
	{Name: "u", DeclaredType: "", Affinity: "BLOB", Declared: false},
}

func TestCanonicalOfNegativeZero(t *testing.T) {
	for _, column := range fuzzColumns[1:4] {
		for _, raw := range []string{"-0.0", "-0", "0", "0.0", "-0e5", "+0"} {
			got, err := Canonical(column, Value{Raw: raw})
			if err != nil {
				t.Fatalf("%s %q: %v", column.DeclaredType, raw, err)
			}
			if got.Raw != "0" {
				t.Errorf("%s %q: canonical %q, want \"0\"", column.DeclaredType, raw, got.Raw)
			}
		}
	}
}

func FuzzCanonicalIsStable(f *testing.F) {
	for _, seed := range []string{"0", "-0", "-0.0", "+5", "5.0", "007", "1e21", "1E-7", ".5", "4.9e-324", "1.7976931348623157e308", "9007199254740993", "-9223372036854775808", "abc", "", "null"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		for _, column := range fuzzColumns {
			once, err := Canonical(column, Value{Raw: raw})
			if err != nil {
				continue
			}
			twice, err := Canonical(column, once)
			if err != nil {
				t.Fatalf("%s %q: canonical %q is refused on a second pass: %v", column.DeclaredType, raw, once.Raw, err)
			}
			if twice != once {
				t.Fatalf("%s %q: canonical %q, then %q", column.DeclaredType, raw, once.Raw, twice.Raw)
			}
		}
	})
}
