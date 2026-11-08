package schema

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/multiqc"
	"github.com/colligo-go/scribo/internal/validate"
)

const (
	SampleColumn  = "sample"
	VersionColumn = "tool_version"
)

var realColumn = db.Column{Name: "value", DeclaredType: "REAL", Affinity: "REAL", Declared: true}

func FromMultiQC(name string, table multiqc.Table, withVersion bool) (db.TableDefinition, error) {
	columns := []db.ColumnDefinition{{Name: SampleColumn, Type: multiqc.TypeText, NotNull: true}}
	if withVersion {
		columns = append(columns, db.ColumnDefinition{Name: VersionColumn, Type: multiqc.TypeText})
	}
	for _, column := range table.Columns {
		columns = append(columns, db.ColumnDefinition{Name: column.Name, Type: column.Type})
	}
	if err := refuseCollisions(columns, "metric"); err != nil {
		return db.TableDefinition{}, err
	}
	return db.TableDefinition{Name: name, Columns: columns, Unique: []string{SampleColumn}}, nil
}

func FromTSV(name string, r io.Reader, keys []string) (db.TableDefinition, error) {
	lines := bufio.NewReader(r)
	header, err := readLine(lines)
	if errors.Is(err, io.EOF) && header == nil {
		return db.TableDefinition{}, invalidTSV("the file is empty; the first line must be the header")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return db.TableDefinition{}, err
	}

	for _, key := range keys {
		if !containsExactly(header, key) {
			return db.TableDefinition{}, exitcode.New(exitcode.Database, exitcode.CodeColumnNotFound,
				fmt.Sprintf("key %s is not a column of the header", key), validate.Suggestion(key, header))
		}
	}

	kinds := make([]string, len(header))
	for number := 2; ; number++ {
		cells, err := readLine(lines)
		if cells != nil {
			if len(cells) != len(header) {
				return db.TableDefinition{}, invalidTSV(fmt.Sprintf("line %d has %d cells and the header has %d", number, len(cells), len(header)))
			}
			for i, cell := range cells {
				kinds[i] = widen(kinds[i], cell)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return db.TableDefinition{}, err
		}
	}

	isKey := map[string]bool{}
	for _, key := range keys {
		isKey[key] = true
	}
	columns := make([]db.ColumnDefinition, len(header))
	for i, name := range header {
		columns[i] = db.ColumnDefinition{Name: name, Type: kinds[i], NotNull: isKey[name]}
		if kinds[i] == "" {
			columns[i].Type = multiqc.TypeText
		}
	}
	if err := refuseCollisions(columns, "column"); err != nil {
		return db.TableDefinition{}, err
	}
	return db.TableDefinition{Name: name, Columns: columns, Unique: keys}, nil
}

func readLine(lines *bufio.Reader) ([]string, error) {
	text, err := lines.ReadString('\n')
	if text == "" {
		return nil, err
	}
	text = strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
	return strings.Split(text, "\t"), err
}

func widen(current, cell string) string {
	if cell == "" || current == multiqc.TypeText {
		return current
	}
	if _, err := db.Coerce(realColumn, db.Value{Raw: cell}); err != nil {
		return multiqc.TypeText
	}
	return multiqc.TypeReal
}

func containsExactly(list []string, text string) bool {
	for _, item := range list {
		if item == text {
			return true
		}
	}
	return false
}

func refuseCollisions(columns []db.ColumnDefinition, noun string) error {
	seen := map[string]string{}
	for _, column := range columns {
		if column.Name == "" {
			return invalidFormat(fmt.Sprintf("a %s has an empty name", noun), "")
		}
		lower := strings.ToLower(column.Name)
		if first, taken := seen[lower]; taken {
			return invalidFormat(fmt.Sprintf("%s %s and %s name the same column", noun, first, column.Name),
				"SQLite compares column names without regard to case")
		}
		seen[lower] = column.Name
	}
	return nil
}

func invalidTSV(message string) *exitcode.Error {
	return invalidFormat(message, "a TSV with the header on the first line and one tab between cells")
}

func invalidFormat(message, context string) *exitcode.Error {
	return exitcode.New(exitcode.File, exitcode.CodeInvalidFormat, message, context)
}
