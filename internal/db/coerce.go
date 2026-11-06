package db

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/colligo-go/scribo/internal/exitcode"
)

var (
	decimalLiteral = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
	integerLiteral = regexp.MustCompile(`^[+-]?[0-9]+$`)
)

// Coerce turns a user-supplied Value into the Go value to bind to column.
//
// A real NULL becomes nil. A numeric column (INTEGER or REAL affinity, or a
// NUMERIC column declared as NUMERIC or DECIMAL) takes a number: int64 when
// the text is an integer, float64 otherwise, and anything that is not a plain
// decimal literal is an invalid_value error. Every other column, dates and
// booleans included, takes the text unchanged: SQLite gives DATE, DATETIME
// and BOOLEAN columns NUMERIC affinity only by elimination, and refusing a
// date for not being a number would be wrong.
//
// The literal grammar is checked before strconv sees the text, because
// strconv.ParseFloat also accepts "NaN", "Inf", hexadecimal floats and
// underscores, none of which belong in a result table.
func Coerce(column Column, v Value) (any, error) {
	if v.IsNull {
		return nil, nil
	}
	if !isNumeric(column) {
		return v.Raw, nil
	}

	if column.Affinity == "INTEGER" {
		if !integerLiteral.MatchString(v.Raw) {
			return nil, notANumber(column, v)
		}
		n, err := strconv.ParseInt(v.Raw, 10, 64)
		if err != nil {
			return nil, notANumber(column, v)
		}
		return n, nil
	}

	if !decimalLiteral.MatchString(v.Raw) {
		return nil, notANumber(column, v)
	}
	if integerLiteral.MatchString(v.Raw) {
		if n, err := strconv.ParseInt(v.Raw, 10, 64); err == nil {
			return n, nil
		}
	}
	f, err := strconv.ParseFloat(v.Raw, 64)
	if err != nil {
		return nil, notANumber(column, v)
	}
	return f, nil
}

func isNumeric(column Column) bool {
	switch column.Affinity {
	case "INTEGER", "REAL":
		return true
	case "NUMERIC":
		declared := strings.ToUpper(column.DeclaredType)
		return strings.Contains(declared, "NUM") || strings.Contains(declared, "DEC")
	}
	return false
}

func notANumber(column Column, v Value) *exitcode.Error {
	return exitcode.New(exitcode.Data, exitcode.CodeInvalidValue,
		fmt.Sprintf("%s is %s and %q is not a number", column.Name, column.Affinity, v.Raw), "")
}
