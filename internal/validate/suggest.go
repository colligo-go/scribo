package validate

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxSuggestionDistance = 2
	maxListedColumns      = 20
)

func Suggestion(name string, columns []string) string {
	return suggestFrom("columns", name, columns)
}

func TableSuggestion(name string, tables []string) string {
	if len(tables) == 0 {
		return "the database has no data table yet"
	}
	return suggestFrom("tables", name, tables)
}

func suggestFrom(label, name string, candidates []string) string {
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)

	best := ""
	bestDistance := maxSuggestionDistance + 1
	for _, candidate := range sorted {
		distance := editDistance(strings.ToLower(name), strings.ToLower(candidate))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if best != "" {
		return fmt.Sprintf("did you mean %q?", best)
	}

	if len(sorted) > maxListedColumns {
		sorted = sorted[:maxListedColumns]
	}
	return label + ": " + strings.Join(sorted, ", ")
}

func editDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}
