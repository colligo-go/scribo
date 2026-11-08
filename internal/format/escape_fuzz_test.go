package format

import (
	"strings"
	"testing"
)

func unescape(t *testing.T, text string) string {
	t.Helper()
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' {
			out.WriteByte(text[i])
			continue
		}
		i++
		if i == len(text) {
			t.Fatalf("%q ends inside an escape", text)
		}
		switch text[i] {
		case '\\':
			out.WriteByte('\\')
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		default:
			t.Fatalf("%q has an unknown escape \\%c", text, text[i])
		}
	}
	return out.String()
}

func FuzzEscape(f *testing.F) {
	for _, seed := range []string{"", `\N`, "a\tb\nc\rd\\e", `\\t`, "\x00", "é"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		escaped := Escape(text)
		if strings.ContainsAny(escaped, "\t\n\r") {
			t.Fatalf("Escape(%q) = %q keeps a separator", text, escaped)
		}
		if escaped == TSVNull {
			t.Fatalf("Escape(%q) is the NULL marker", text)
		}
		if got := unescape(t, escaped); got != text {
			t.Fatalf("unescape(Escape(%q)) = %q", text, got)
		}
	})
}
