package history

import (
	"sort"

	"github.com/colligo-go/scribo/internal/db"
)

func StateAt(rows []db.HistoryRow, at string) (db.Row, []string) {
	state := db.Row{}
	firstAfter := map[string]string{}
	for _, row := range rows {
		if row.ChangedAt <= at {
			state[row.Field] = row.New
			continue
		}
		if _, seen := firstAfter[row.Field]; !seen {
			firstAfter[row.Field] = row.Source
		}
	}

	var approximate []string
	for field, source := range firstAfter {
		if source == "reconciled" {
			approximate = append(approximate, field)
		}
	}
	sort.Strings(approximate)
	return state, approximate
}
