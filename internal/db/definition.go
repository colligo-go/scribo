package db

// ColumnDefinition is one column of a table that `scribo schema` prints.
// Type is a type a STRICT table accepts: INTEGER, REAL or TEXT.
type ColumnDefinition struct {
	Name    string
	Type    string
	NotNull bool
}

// TableDefinition is a data table that `scribo schema` prints as DDL for
// the user to review and run. Scribo never runs it: the data table is
// always the user's. Unique names the columns of its one UNIQUE
// constraint, the key of every write to it.
type TableDefinition struct {
	Name    string
	Columns []ColumnDefinition
	Unique  []string
}
