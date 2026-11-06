package history

import (
	"reflect"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
)

func TestReconcile(t *testing.T) {
	null := db.Value{IsNull: true}
	text := func(raw string) db.Value { return db.Value{Raw: raw} }
	tests := []struct {
		name      string
		projected db.Row
		current   db.Row
		found     bool
		want      []db.FieldChange
	}{
		{"equal", db.Row{"a": text("1")}, db.Row{"a": text("1")}, true, nil},
		{"different", db.Row{"a": text("1")}, db.Row{"a": text("2")}, true,
			[]db.FieldChange{{Field: "a", OldValue: text("1"), NewValue: text("2")}}},
		{"current NULL", db.Row{"a": text("1")}, db.Row{"a": null}, true,
			[]db.FieldChange{{Field: "a", OldValue: text("1"), NewValue: null}}},
		{"projected NULL, current set", db.Row{"a": null}, db.Row{"a": text("")}, true,
			[]db.FieldChange{{Field: "a", OldValue: null, NewValue: text("")}}},
		{"both NULL", db.Row{"a": null}, db.Row{"a": null}, true, nil},
		{"record deleted", db.Row{"b": text("2"), "a": text("1")}, nil, false,
			[]db.FieldChange{
				{Field: "a", OldValue: text("1"), NewValue: null},
				{Field: "b", OldValue: text("2"), NewValue: null},
			}},
		{"record deleted, projected NULL", db.Row{"a": null}, nil, false, nil},
		{"no history", db.Row{}, db.Row{"a": text("1")}, true, nil},
		{"field without history ignored", db.Row{"a": text("1")}, db.Row{"a": text("1"), "z": text("9")}, true, nil},
		{"alphabetical order", db.Row{"note": text("x"), "Mapped_Reads": text("1")}, db.Row{"note": text("y"), "Mapped_Reads": text("2")}, true,
			[]db.FieldChange{
				{Field: "Mapped_Reads", OldValue: text("1"), NewValue: text("2")},
				{Field: "note", OldValue: text("x"), NewValue: text("y")},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(tt.projected, tt.current, tt.found)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Reconcile = %+v, want %+v", got, tt.want)
			}
		})
	}
}
