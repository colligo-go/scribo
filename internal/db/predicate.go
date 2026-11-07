package db

// Op is the comparison of one filter of a read.
type Op int

const (
	// OpEq keeps the rows whose field equals the value.
	OpEq Op = iota
	// OpNe keeps the rows whose field does not equal the value, NULL
	// included: it is the complement of OpEq.
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	// OpIsNull keeps the rows whose field is a real NULL. The value is
	// ignored.
	OpIsNull
	// OpNotNull keeps the rows whose field is not NULL. The value is
	// ignored.
	OpNotNull
)

// String is the operator as the user writes it in --where, or the SQL
// words for the two NULL tests.
func (o Op) String() string {
	switch o {
	case OpEq:
		return "="
	case OpNe:
		return "!="
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	case OpIsNull:
		return "IS NULL"
	case OpNotNull:
		return "IS NOT NULL"
	}
	return "unknown"
}

// Ordering reports whether the operator compares by order (<, <=, >, >=),
// which is only meaningful for numbers unless text order is asked for.
func (o Op) Ordering() bool {
	return o == OpLt || o == OpLe || o == OpGt || o == OpGe
}

// Predicate is one filter of a read; a read joins its predicates by AND.
// Value is bound as a number when the column is numeric and AsText is
// false, and as text otherwise. AsText also compares the column as text,
// CAST("col" AS TEXT), whatever its type.
type Predicate struct {
	Field  string
	Op     Op
	Value  Value
	AsText bool
}

// Rows is the result of a read, one Row per record, in the order the read
// returned them.
type Rows []Row
