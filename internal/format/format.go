package format

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/colligo-go/scribo/internal/db"
)

const (
	TSVNull    = `\N`
	PrettyNull = "∅"
)

var escaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

func Escape(text string) string {
	return escaper.Replace(text)
}

func TSV(w io.Writer, columns []string, rows [][]db.Value) error {
	out := bufio.NewWriter(w)
	header := make([]string, len(columns))
	for i, name := range columns {
		header[i] = Escape(name)
	}
	out.WriteString(strings.Join(header, "\t") + "\n")
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			cells[i] = TSVNull
			if !value.IsNull {
				cells[i] = Escape(value.Raw)
			}
		}
		out.WriteString(strings.Join(cells, "\t") + "\n")
	}
	return out.Flush()
}

func CSV(w io.Writer, columns []string, rows [][]db.Value) error {
	out := bufio.NewWriter(w)
	header := make([]string, len(columns))
	for i, name := range columns {
		header[i] = csvField(name)
	}
	out.WriteString(strings.Join(header, ",") + "\n")
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			if !value.IsNull {
				cells[i] = csvField(value.Raw)
			}
		}
		out.WriteString(strings.Join(cells, ",") + "\n")
	}
	return out.Flush()
}

func csvField(text string) string {
	if text == "" {
		return `""`
	}
	if !strings.ContainsAny(text, ",\"\r\n") {
		return text
	}
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

func JSON(w io.Writer, columns []string, rows [][]db.Value) error {
	out := bufio.NewWriter(w)
	names := make([]string, len(columns))
	for i, name := range columns {
		encoded, err := jsonString(name)
		if err != nil {
			return err
		}
		names[i] = encoded
	}
	for _, row := range rows {
		var line strings.Builder
		line.WriteString("{")
		for i, value := range row {
			if i > 0 {
				line.WriteString(",")
			}
			line.WriteString(names[i] + ":")
			if value.IsNull {
				line.WriteString("null")
				continue
			}
			encoded, err := jsonString(value.Raw)
			if err != nil {
				return err
			}
			line.WriteString(encoded)
		}
		line.WriteString("}\n")
		out.WriteString(line.String())
	}
	return out.Flush()
}

func jsonString(text string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(text); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

func Pretty(w io.Writer, columns []string, rows [][]db.Value) error {
	lines := make([][]string, 0, len(rows)+2)
	header := make([]string, len(columns))
	widths := make([]int, len(columns))
	for i, name := range columns {
		header[i] = Escape(name)
		widths[i] = utf8.RuneCountInString(header[i])
	}
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			cells[i] = PrettyNull
			if !value.IsNull {
				cells[i] = Escape(value.Raw)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(cells[i]))
		}
		lines = append(lines, cells)
	}
	dashes := make([]string, len(columns))
	for i, width := range widths {
		dashes[i] = strings.Repeat("-", max(width, 1))
	}

	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cells := range append([][]string{header, dashes}, lines...) {
		io.WriteString(table, strings.Join(cells, "\t")+"\n")
	}
	return table.Flush()
}
