package history

import (
	"reflect"
	"testing"

	"github.com/colligo-go/scribo/internal/db"
)

func historyRow(field, source, changedAt, newRaw string) db.HistoryRow {
	return db.HistoryRow{Field: field, Source: source, ChangedAt: changedAt, New: db.Value{Raw: newRaw}}
}

func TestStateAt(t *testing.T) {
	rows := []db.HistoryRow{
		historyRow("n", "scribo", "2026-11-06T10:00:00.000Z", "12"),
		historyRow("note", "scribo", "2026-11-06T10:00:00.000Z", "ok"),
		historyRow("n", "scribo", "2026-11-06T11:00:00.000Z", "13"),
		historyRow("note", "reconciled", "2026-11-06T12:00:00.000Z", "edited"),
		historyRow("n", "scribo", "2026-11-06T12:00:00.000Z", "14"),
	}
	tests := []struct {
		name        string
		at          string
		state       db.Row
		approximate []string
	}{
		{"before the first row", "2026-11-06T09:59:59.999Z", db.Row{}, nil},
		{"between rows", "2026-11-06T10:30:00.000Z", db.Row{"n": {Raw: "12"}, "note": {Raw: "ok"}}, []string{"note"}},
		{"exactly at a row", "2026-11-06T11:00:00.000Z", db.Row{"n": {Raw: "13"}, "note": {Raw: "ok"}}, []string{"note"}},
		{"after the last row", "2026-11-07T00:00:00.000Z", db.Row{"n": {Raw: "14"}, "note": {Raw: "edited"}}, nil},
		{"reconciled row before at", "2026-11-06T12:00:00.000Z", db.Row{"n": {Raw: "14"}, "note": {Raw: "edited"}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, approximate := StateAt(rows, tt.at)
			if !reflect.DeepEqual(state, tt.state) {
				t.Errorf("state = %v, want %v", state, tt.state)
			}
			if !reflect.DeepEqual(approximate, tt.approximate) {
				t.Errorf("approximate = %v, want %v", approximate, tt.approximate)
			}
		})
	}
}

func TestStateAtOnlyTheFirstRowAfterAtDecides(t *testing.T) {
	rows := []db.HistoryRow{
		historyRow("n", "scribo", "2026-11-06T10:00:00.000Z", "1"),
		historyRow("n", "scribo", "2026-11-06T11:00:00.000Z", "2"),
		historyRow("n", "reconciled", "2026-11-06T12:00:00.000Z", "3"),
	}
	if _, approximate := StateAt(rows, "2026-11-06T10:30:00.000Z"); approximate != nil {
		t.Errorf("approximate = %v, want none: the next change was Scribo's own", approximate)
	}
}
