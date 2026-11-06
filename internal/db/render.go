package db

import (
	"fmt"
	"strconv"

	"github.com/colligo-go/scribo/internal/exitcode"
)

// Render turns a value read from the database into the text form Scribo
// stores in the history and compares. nil is a real NULL; integers use base
// 10; floats use the shortest text that reads back as the same number, so
// 3.0 renders as "3" and 94.3 as "94.3". Any other type is a bug.
func Render(x any) (Value, error) {
	switch v := x.(type) {
	case nil:
		return Value{IsNull: true}, nil
	case int64:
		return Value{Raw: strconv.FormatInt(v, 10)}, nil
	case float64:
		return Value{Raw: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case string:
		return Value{Raw: v}, nil
	case []byte:
		return Value{Raw: string(v)}, nil
	default:
		return Value{}, exitcode.New(exitcode.System, exitcode.CodeInternal,
			fmt.Sprintf("cannot render a value of type %T", x), "")
	}
}

// Canonical is the form of v that the database would hold after a write to
// column: coerced, then rendered. "94.30" and "94.3" in a REAL column are
// the same canonical value, so writing one over the other is no change.
func Canonical(column Column, v Value) (Value, error) {
	bound, err := Coerce(column, v)
	if err != nil {
		return Value{}, err
	}
	return Render(bound)
}
