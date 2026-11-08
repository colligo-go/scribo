package multiqc

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
)

var integerText = regexp.MustCompile(`^[+-]?[0-9]+$`)

type kind int

const (
	kindInteger kind = iota
	kindReal
	kindText
)

type cell struct {
	text   string
	kind   kind
	absent bool
	group  string
}

func cellOf(value any) (cell, bool) {
	switch v := value.(type) {
	case nil:
		return cell{absent: true}, true
	case json.Number:
		text := v.String()
		if integerText.MatchString(text) {
			if _, err := strconv.ParseInt(text, 10, 64); err == nil {
				return cell{text: text, kind: kindInteger}, true
			}
		}
		return cell{text: text, kind: kindReal}, true
	case string:
		return cell{text: v, kind: kindText}, true
	case bool:
		return cell{text: strconv.FormatBool(v), kind: kindText}, true
	}
	return cell{}, false
}

type tableBuilder struct {
	cells   map[string]map[string]cell
	samples map[string]bool
	skipped map[string]bool
}

func newTableBuilder() *tableBuilder {
	return &tableBuilder{
		cells:   map[string]map[string]cell{},
		samples: map[string]bool{},
		skipped: map[string]bool{},
	}
}

func (b *tableBuilder) touch(sample string) {
	b.samples[sample] = true
}

func (b *tableBuilder) add(group, sample, column, field string, value any) error {
	b.touch(sample)
	if b.skipped[column] {
		return nil
	}
	c, ok := cellOf(value)
	if !ok {
		b.skipped[column] = true
		delete(b.cells, column)
		return nil
	}
	c.group = group
	bySample := b.cells[column]
	if bySample == nil {
		bySample = map[string]cell{}
		b.cells[column] = bySample
	}
	if previous, seen := bySample[sample]; seen {
		if previous.absent != c.absent || previous.text != c.text {
			return disagreement(previous.group, group, field, sample)
		}
		return nil
	}
	bySample[sample] = c
	return nil
}

func (b *tableBuilder) table() Table {
	table := Table{Rows: make(map[string]map[string]string, len(b.samples))}
	for sample := range b.samples {
		table.Rows[sample] = map[string]string{}
	}
	names := make([]string, 0, len(b.cells))
	for name := range b.cells {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		table.Columns = append(table.Columns, Column{Name: name, Type: columnType(b.cells[name])})
		for sample, c := range b.cells[name] {
			if !c.absent {
				table.Rows[sample][name] = c.text
			}
		}
	}
	for name := range b.skipped {
		table.Skipped = append(table.Skipped, name)
	}
	sort.Strings(table.Skipped)
	return table
}

func columnType(cells map[string]cell) string {
	widest := kindInteger
	present := false
	for _, c := range cells {
		if c.absent {
			continue
		}
		present = true
		widest = max(widest, c.kind)
	}
	if !present {
		return TypeText
	}
	switch widest {
	case kindInteger:
		return TypeInteger
	case kindReal:
		return TypeReal
	}
	return TypeText
}
