package history

import (
	"math"
	"sort"

	"github.com/colligo-go/scribo/internal/db"
)

type Cutoff struct {
	ChangedAt string
	ID        int64
}

func AtMoment(changedAt string) Cutoff {
	return Cutoff{ChangedAt: changedAt, ID: math.MaxInt64}
}

func (c Cutoff) includes(row db.HistoryRow) bool {
	return row.ChangedAt < c.ChangedAt || (row.ChangedAt == c.ChangedAt && row.ID <= c.ID)
}

func StateAt(rows []db.HistoryRow, cutoff Cutoff) (db.Row, []string) {
	state := db.Row{}
	firstAfter := map[string]string{}
	for _, row := range rows {
		if cutoff.includes(row) {
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
