package validate

import (
	"fmt"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

var twoCharacterOps = map[string]db.Op{">=": db.OpGe, "<=": db.OpLe, "!=": db.OpNe}

var oneCharacterOps = map[byte]db.Op{'=': db.OpEq, '>': db.OpGt, '<': db.OpLt}

func ParseWhere(text string) (string, db.Op, string, error) {
	for i := 0; i < len(text); i++ {
		if i+2 <= len(text) {
			if op, ok := twoCharacterOps[text[i:i+2]]; ok {
				return splitWhere(text, i, 2, op)
			}
		}
		if op, ok := oneCharacterOps[text[i]]; ok {
			return splitWhere(text, i, 1, op)
		}
	}
	return "", 0, "", whereUsage(text)
}

func splitWhere(text string, at, width int, op db.Op) (string, db.Op, string, error) {
	if at == 0 {
		return "", 0, "", whereUsage(text)
	}
	return text[:at], op, text[at+width:], nil
}

func whereUsage(text string) error {
	return exitcode.New(exitcode.Usage, exitcode.CodeUsage,
		fmt.Sprintf("--where %q must be FIELD<op>VALUE with op one of = != > < >= <=", text),
		"for example --where \"Alignment_Rate>90\"; a NULL is filtered with --is-null FIELD")
}

type Filters struct {
	Where     []string
	IsNull    []string
	NotNull   []string
	ForceText bool
}

func (f Filters) Fields() ([]string, error) {
	var names []string
	for _, text := range f.Where {
		field, _, _, err := ParseWhere(text)
		if err != nil {
			return nil, err
		}
		names = append(names, field)
	}
	names = append(names, f.IsNull...)
	return append(names, f.NotNull...), nil
}

func Predicates(columns map[string]db.Column, filters Filters) ([]db.Predicate, error) {
	var predicates []db.Predicate
	for _, text := range filters.Where {
		field, op, value, err := ParseWhere(text)
		if err != nil {
			return nil, err
		}
		predicate, err := comparison(columns[field], op, value, filters.ForceText)
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, predicate)
	}
	for _, field := range filters.IsNull {
		predicates = append(predicates, db.Predicate{Field: field, Op: db.OpIsNull})
	}
	for _, field := range filters.NotNull {
		predicates = append(predicates, db.Predicate{Field: field, Op: db.OpNotNull})
	}
	return predicates, nil
}

func comparison(column db.Column, op db.Op, value string, forceText bool) (db.Predicate, error) {
	predicate := db.Predicate{Field: column.Name, Op: op, Value: db.Value{Raw: value}, AsText: forceText}
	switch {
	case forceText:
		return predicate, nil
	case db.IsNumeric(column):
		if _, err := db.Coerce(column, predicate.Value); err != nil {
			return db.Predicate{}, err
		}
		return predicate, nil
	case op.Ordering():
		return db.Predicate{}, exitcode.New(exitcode.Database, exitcode.CodeUntypedComparison,
			fmt.Sprintf("%s is not a numeric column; %s would compare text", column.Name, op),
			"pass --force-text to compare as text (ISO dates sort correctly as text)")
	}
	return predicate, nil
}
