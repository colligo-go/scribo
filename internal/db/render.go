package db

import (
	"fmt"
	"math"
	"strconv"

	"github.com/colligo-go/scribo/internal/exitcode"
)

// Render turns a value read from the database into the text form Scribo
// stores in the history and compares. nil is a real NULL; integers use base
// 10; floats use the shortest digits that read back as the same number, by
// the rule of ECMAScript's Number.prototype.toString: plain decimal when
// 1e-7 <= |v| < 1e21, so 3.0 renders as "3", 94.3 as "94.3" and 12000000.0
// as "12000000", and exponent notation outside that range ("1e+21",
// "1e-08"). A count stored in a REAL column never shows up as "1.2e+07".
// Negative zero renders as "0": it compares equal to zero in SQLite, and
// "-0" would read back as the integer 0, giving one record two keys. Any
// other type is a bug.
func Render(x any) (Value, error) {
	switch v := x.(type) {
	case nil:
		return Value{IsNull: true}, nil
	case int64:
		return Value{Raw: strconv.FormatInt(v, 10)}, nil
	case float64:
		return Value{Raw: formatFloat(v)}, nil
	case string:
		return Value{Raw: v}, nil
	case []byte:
		return Value{Raw: string(v)}, nil
	default:
		return Value{}, exitcode.New(exitcode.System, exitcode.CodeInternal,
			fmt.Sprintf("cannot render a value of type %T", x), "")
	}
}

func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	if magnitude := math.Abs(v); magnitude >= 1e-7 && magnitude < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
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
