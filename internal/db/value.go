package db

// Value is a single field value as the user supplied it. Raw holds the text
// exactly as given; IsNull marks a real SQL NULL and, when true, Raw is
// ignored. The text "null" is an ordinary Value{Raw: "null"}, never a NULL.
type Value struct {
	Raw    string
	IsNull bool
}

// Key identifies one record: the field names of its key columns mapped to
// their values. The map has no order of its own; ordering is applied where a
// canonical form is built.
type Key map[string]Value

// Values maps the non-key field names of one write to their new values.
type Values map[string]Value

// Row is the stored state of one record: field name to value, as read back
// from the database and rendered to text.
type Row map[string]Value

// Column describes one column of a data table as SQLite reports it.
type Column struct {
	Name string

	// DeclaredType is the type text exactly as written in the CREATE TABLE,
	// such as "DECIMAL(10,2)". It is empty for a column declared without one.
	DeclaredType string

	// Affinity is the SQLite type affinity derived from DeclaredType:
	// INTEGER, TEXT, BLOB, REAL or NUMERIC.
	Affinity string

	Nullable bool

	// Declared is false when the column has no declared type, which makes
	// numeric comparison against it unreliable.
	Declared bool
}
