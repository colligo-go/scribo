package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colligo-go/scribo/internal/db"
)

var hardColumns = []string{"name", "value"}

var hardRows = [][]db.Value{
	{{Raw: "null"}, {IsNull: true}},
	{{Raw: "text null"}, {Raw: "null"}},
	{{Raw: "empty"}, {Raw: ""}},
	{{Raw: "controls"}, {Raw: "a\tb\nc\rd"}},
	{{Raw: "backslash N"}, {Raw: `\N`}},
	{{Raw: "comma"}, {Raw: `x,"y"`}},
	{{Raw: "html"}, {Raw: "<&>"}},
	{{Raw: "accent"}, {Raw: "é"}},
}

func render(t *testing.T, write func(*bytes.Buffer) error) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := write(&buffer); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

func TestTSV(t *testing.T) {
	got := render(t, func(b *bytes.Buffer) error { return TSV(b, hardColumns, hardRows) })
	want := "name\tvalue\n" +
		"null\t\\N\n" +
		"text null\tnull\n" +
		"empty\t\n" +
		"controls\ta\\tb\\nc\\rd\n" +
		"backslash N\t\\\\N\n" +
		"comma\tx,\"y\"\n" +
		"html\t<&>\n" +
		"accent\té\n"
	if got != want {
		t.Errorf("TSV:\n%q\nwant:\n%q", got, want)
	}
}

func TestEscapeKeepsALiteralBackslashNApartFromNull(t *testing.T) {
	if got := Escape(`\N`); got == TSVNull || got != `\\N` {
		t.Errorf("Escape(`\\N`) = %q", got)
	}
}

func TestCSV(t *testing.T) {
	got := render(t, func(b *bytes.Buffer) error { return CSV(b, hardColumns, hardRows) })
	want := "name,value\n" +
		"null,\n" +
		"text null,null\n" +
		"empty,\"\"\n" +
		"controls,\"a\tb\nc\rd\"\n" +
		"backslash N,\\N\n" +
		"comma,\"x,\"\"y\"\"\"\n" +
		"html,<&>\n" +
		"accent,é\n"
	if got != want {
		t.Errorf("CSV:\n%q\nwant:\n%q", got, want)
	}
}

func TestJSON(t *testing.T) {
	got := render(t, func(b *bytes.Buffer) error { return JSON(b, hardColumns, hardRows) })
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != len(hardRows) {
		t.Fatalf("got %d lines", len(lines))
	}
	if lines[0] != `{"name":"null","value":null}` || lines[6] != `{"name":"html","value":"<&>"}` {
		t.Errorf("lines = %q", lines)
	}
	for i, line := range lines {
		var object map[string]*string
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		want := hardRows[i][1]
		if value := object["value"]; (value == nil) != want.IsNull || (value != nil && *value != want.Raw) {
			t.Errorf("line %s decodes to %v, want %+v", line, value, want)
		}
	}
}

func TestJSONKeepsColumnOrder(t *testing.T) {
	got := render(t, func(b *bytes.Buffer) error {
		return JSON(b, []string{"z", "a"}, [][]db.Value{{{Raw: "1"}, {Raw: "2"}}})
	})
	if got != `{"z":"1","a":"2"}`+"\n" {
		t.Errorf("JSON = %q", got)
	}
}

func TestPretty(t *testing.T) {
	got := render(t, func(b *bytes.Buffer) error { return Pretty(b, []string{"value", "name"}, swap(hardRows)) })
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != len(hardRows)+2 {
		t.Fatalf("got %d lines:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[1], "----") || !strings.HasPrefix(lines[2], PrettyNull+" ") || !strings.Contains(got, `a\tb\nc\rd`) {
		t.Errorf("pretty:\n%s", got)
	}
	column := strings.Index(lines[0], "name")
	for i, line := range lines[2:] {
		name := hardRows[i][0].Raw
		at := utf8.RuneCountInString(line[:strings.LastIndex(line, name)])
		if at != column {
			t.Errorf("line %q puts the name at %d, the header at %d", line, at, column)
		}
	}
}

func swap(rows [][]db.Value) [][]db.Value {
	out := make([][]db.Value, len(rows))
	for i, row := range rows {
		out[i] = []db.Value{row[1], row[0]}
	}
	return out
}
