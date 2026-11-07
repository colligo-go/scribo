package validate

import (
	"testing"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func keyOf(names ...string) db.Key {
	key := db.Key{}
	for _, name := range names {
		key[name] = db.Value{Raw: "x"}
	}
	return key
}

func TestKey(t *testing.T) {
	composite := [][]string{{"replicate", "sample"}}
	tests := []struct {
		name        string
		uniqueKeys  [][]string
		key         db.Key
		wantCode    string
		wantMessage string
		wantContext string
	}{
		{"exact", composite, keyOf("sample", "replicate"), "", "", ""},
		{"another order", composite, keyOf("replicate", "sample"), "", "", ""},
		{"different case", composite, keyOf("SAMPLE", "Replicate"), "", "", ""},
		{"one missing", composite, keyOf("sample"), "key_mismatch", "the key of rnaseq_qc is (replicate, sample); replicate is missing", ""},
		{"one extra", composite, keyOf("sample", "replicate", "lane"), "key_mismatch", "the key of rnaseq_qc is (replicate, sample); lane is not part of it", ""},
		{"missing and extra", composite, keyOf("sample", "lane"), "key_mismatch", "the key of rnaseq_qc is (replicate, sample); replicate is missing; lane is not part of it", ""},
		{"second of two matches", [][]string{{"id"}, {"replicate", "sample"}}, keyOf("sample", "replicate"), "", "", ""},
		{"two keys, none matches", [][]string{{"id"}, {"replicate", "sample"}}, keyOf("sample"), "key_mismatch",
			"the key of rnaseq_qc is (replicate, sample); replicate is missing", "unique keys: (id); (replicate, sample)"},
		{"tie goes to the first", [][]string{{"a"}, {"b"}}, keyOf("c"), "key_mismatch", "the key of rnaseq_qc is (a); a is missing; c is not part of it", "unique keys: (a); (b)"},
		{"no unique key", nil, keyOf("sample"), "no_unique_key", "rnaseq_qc has no unique key", "declare the key columns UNIQUE, for example UNIQUE(sample, replicate)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Key("rnaseq_qc", tt.uniqueKeys, tt.key)

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Key = %v, want nil", err)
				}
				return
			}
			domain, ok := err.(*exitcode.Error)
			if !ok {
				t.Fatalf("Key = %v, want an *exitcode.Error", err)
			}
			if domain.Status != exitcode.Database || domain.Code != tt.wantCode {
				t.Errorf("status %d code %q, want %d %q", domain.Status, domain.Code, exitcode.Database, tt.wantCode)
			}
			if domain.Message != tt.wantMessage || domain.Context != tt.wantContext {
				t.Errorf("message %q context %q, want %q and %q", domain.Message, domain.Context, tt.wantMessage, tt.wantContext)
			}
		})
	}
}
