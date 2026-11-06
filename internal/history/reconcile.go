package history

import (
	"sort"

	"github.com/colligo-go/scribo/internal/db"
)

func Reconcile(projected, current db.Row, found bool) []db.FieldChange {
	fields := make([]string, 0, len(projected))
	for field := range projected {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	var changes []db.FieldChange
	for _, field := range fields {
		asserted := projected[field]
		actual := db.Value{IsNull: true}
		if found {
			actual = current[field]
		}
		if asserted.Equal(actual) {
			continue
		}
		changes = append(changes, db.FieldChange{Field: field, OldValue: asserted, NewValue: actual})
	}
	return changes
}
