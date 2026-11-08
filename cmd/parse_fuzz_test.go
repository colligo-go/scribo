package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func requireTyped(t *testing.T, input string, err error) {
	t.Helper()
	var domain *exitcode.Error
	if !errors.As(err, &domain) {
		t.Fatalf("%q: error %v is not an *exitcode.Error", input, err)
	}
}

func FuzzParseKeys(f *testing.F) {
	for _, seed := range []string{"sample=S1", "a=b=c", "=x", "x=", "x", "", "note=a,b", "é=ü", "k=\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		key, err := parseKeys([]string{text}, "upsert")
		if err != nil {
			requireTyped(t, text, err)
			return
		}
		if len(key) != 1 {
			t.Fatalf("%q: %d key fields, want 1", text, len(key))
		}
		for name, value := range key {
			if name == "" || strings.Contains(name, "=") || name+"="+value.Raw != text || value.IsNull {
				t.Fatalf("%q: parsed as %q = %+v", text, name, value)
			}
		}
	})
}

func FuzzParseAt(f *testing.F) {
	for _, seed := range []string{
		"2026-11-06T12:00:00Z", "2026-11-06T14:00:00.123456+02:00", "2026-11-06", "ontem", "run:7", "run:0",
		"run:-1", "run:", "run:9223372036854775808", "0000-01-01T00:00:00Z", "0000-01-01T00:00:00+00:01", "9999-12-31T23:59:59-23:59", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		moment, run, err := parseAt(text)
		if err != nil {
			requireTyped(t, text, err)
			return
		}
		switch {
		case run > 0:
			if moment != "" || !strings.HasPrefix(text, "run:") {
				t.Fatalf("%q: moment %q, run %d", text, moment, run)
			}
		case text == "":
			if moment != "" {
				t.Fatalf("empty --at gave moment %q", moment)
			}
		default:
			if len(moment) != len(timestampLayout) {
				t.Fatalf("%q: moment %q does not have the stored layout, so it would not compare as text with changed_at", text, moment)
			}
			again, _, err := parseAt(moment)
			if err != nil || again != moment {
				t.Fatalf("%q: moment %q reparses as %q, %v", text, moment, again, err)
			}
			if _, err := time.Parse(timestampLayout, moment); err != nil {
				t.Fatalf("%q: moment %q: %v", text, moment, err)
			}
		}
	})
}
