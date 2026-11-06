package db

import "strings"

// AffinityOf returns the SQLite type affinity of a declared column type,
// applying the rules of section 3.1 of https://sqlite.org/datatype3.html in
// the order SQLite does: the first rule that matches wins, so "BIGINT" is
// INTEGER and "CHARINT" is INTEGER too, not TEXT. ANY, the type of a
// flexible column in a STRICT table, has no affinity and is reported as BLOB.
func AffinityOf(declared string) string {
	upper := strings.ToUpper(declared)
	switch {
	case strings.Contains(upper, "INT"):
		return "INTEGER"
	case strings.Contains(upper, "CHAR"), strings.Contains(upper, "CLOB"), strings.Contains(upper, "TEXT"):
		return "TEXT"
	case strings.Contains(upper, "BLOB"), strings.TrimSpace(upper) == "":
		return "BLOB"
	case strings.Contains(upper, "REAL"), strings.Contains(upper, "FLOA"), strings.Contains(upper, "DOUB"):
		return "REAL"
	case strings.TrimSpace(upper) == "ANY":
		return "BLOB"
	default:
		return "NUMERIC"
	}
}
