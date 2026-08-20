package dedup

import "math"

// jaroWinklerPrefixWeight and jaroWinklerMaxPrefix are Winkler's own
// standard constants (1990) — no reason to deviate from the values every
// reference implementation uses.
const (
	jaroWinklerPrefixWeight = 0.1
	jaroWinklerMaxPrefix    = 4
)

// JaroWinkler returns the Jaro-Winkler similarity of a and b, in [0, 1].
// Chosen over Levenshtein for docs/08 section 3.2's own reason: it weights
// common prefixes, and job titles share prefixes ("Software Engineering
// Intern" vs "Software Engineering Intern - Summer 2027" scores 0.94 —
// Levenshtein would rate the same pair much lower on raw edit distance).
func JaroWinkler(a, b string) float64 {
	jaro := jaroSimilarity(a, b)
	if jaro == 0 {
		return 0
	}

	prefixLen := commonPrefixLength(a, b, jaroWinklerMaxPrefix)
	return jaro + float64(prefixLen)*jaroWinklerPrefixWeight*(1-jaro)
}

func jaroSimilarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 && lb == 0 {
		return 1
	}
	if la == 0 || lb == 0 {
		return 0
	}

	matchDistance := int(math.Floor(float64(max(la, lb))/2.0)) - 1
	if matchDistance < 0 {
		matchDistance = 0
	}

	aMatched := make([]bool, la)
	bMatched := make([]bool, lb)

	matches := 0
	for i := range ra {
		start := max(0, i-matchDistance)
		end := min(i+matchDistance+1, lb)
		for j := start; j < end; j++ {
			if bMatched[j] || ra[i] != rb[j] {
				continue
			}
			aMatched[i] = true
			bMatched[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	transpositions := 0
	k := 0
	for i := range ra {
		if !aMatched[i] {
			continue
		}
		for !bMatched[k] {
			k++
		}
		if ra[i] != rb[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	return (m/float64(la) + m/float64(lb) + (m-float64(transpositions)/2)/m) / 3
}

func commonPrefixLength(a, b string, maxLen int) int {
	ra, rb := []rune(a), []rune(b)
	n := min(len(ra), len(rb), maxLen)
	i := 0
	for i < n && ra[i] == rb[i] {
		i++
	}
	return i
}
