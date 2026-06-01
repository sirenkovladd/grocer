package receipt

import (
	"math"
	"strings"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/store"
)

type Matcher struct {
	store *store.Store
}

func NewMatcher(store *store.Store) *Matcher {
	return &Matcher{store: store}
}

func (m *Matcher) FindMatch(name string) (*domain.Item, float64, error) {
	items, err := m.store.ListItems()
	if err != nil {
		return nil, 0, err
	}

	normalized := normalizeName(name)

	var bestMatch *domain.Item
	var bestScore float64

	for _, item := range items {
		score := calculateSimilarity(normalized, item.Normalized)
		if score > bestScore {
			bestScore = score
			bestMatch = item
		}

		for _, alias := range item.Aliases {
			aliasScore := calculateSimilarity(normalized, normalizeName(alias))
			if aliasScore > bestScore {
				bestScore = aliasScore
				bestMatch = item
			}
		}
	}

	if bestMatch == nil {
		return nil, 0, nil
	}

	return bestMatch, bestScore, nil
}

func calculateSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)

	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	set2 := make(map[string]bool)
	for _, w := range words2 {
		set2[w] = true
	}

	intersection := 0
	for w := range set1 {
		if set2[w] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0
	}

	jaccard := float64(intersection) / float64(union)

	levDistance := levenshteinDistance(s1, s2)
	maxLen := math.Max(float64(len(s1)), float64(len(s2)))
	levSimilarity := 1.0 - float64(levDistance)/maxLen

	return 0.7*jaccard + 0.3*levSimilarity
}

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			matrix[i][j] = min3(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), " ")
	return name
}
