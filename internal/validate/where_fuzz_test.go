package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func FuzzParseWhere(f *testing.F) {
	for _, seed := range []string{"a=1", "a>=1", "a<=b", "a!=", "a=>b", "x<y=1", "=1", ">", "", "a!b=c", "Alignment_Rate>90", "é<ü"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		field, op, value, err := ParseWhere(text)
		if err != nil {
			var domain *exitcode.Error
			if !errors.As(err, &domain) {
				t.Fatalf("%q: error %v is not an *exitcode.Error", text, err)
			}
			return
		}
		if field == "" || strings.ContainsAny(field, "=<>") || strings.Contains(field+"!", "!=") {
			t.Fatalf("%q: field %q holds an operator", text, field)
		}
		if field+op.String()+value != text {
			t.Fatalf("%q: recomposed as %q", text, field+op.String()+value)
		}
	})
}
