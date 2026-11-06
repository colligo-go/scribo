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
