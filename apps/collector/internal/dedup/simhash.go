package dedup

import (
	"crypto/sha256"
	"encoding/binary"
	"math/bits"
	"strings"
)

// simhashShingleSize is docs/08 section 3.2's own spec: "64-bit SimHash
// over 5-token shingles, weighted by term frequency."
const simhashShingleSize = 5

// SimHash computes a 64-bit SimHash over 5-token shingles of text,
// weighted by shingle term frequency. Chosen over MinHash for the same
// reason docs/08 gives: a single 64-bit integer indexes in Postgres as a
// plain BIGINT and compares with XOR + popcount, where MinHash would need
// multiple hash bands and a more complex index.
func SimHash(text string) uint64 {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return 0
	}

	shingles := make(map[string]int)
	if len(tokens) < simhashShingleSize {
		shingles[strings.Join(tokens, " ")] = 1
	} else {
		for i := 0; i+simhashShingleSize <= len(tokens); i++ {
			shingles[strings.Join(tokens[i:i+simhashShingleSize], " ")]++
		}
	}

	var weights [64]int
	for shingle, freq := range shingles {
		h := hash64(shingle)
		for bit := range 64 {
			if h&(1<<uint(bit)) != 0 {
				weights[bit] += freq
			} else {
				weights[bit] -= freq
			}
		}
	}

	var result uint64
	for bit, w := range weights {
		if w > 0 {
			result |= 1 << uint(bit)
		}
	}
	return result
}

// HammingDistance is the bit count of a XOR b — SimHash similarity is
// measured by how many of the 64 bits differ, per docs/08's Gate 3
// thresholds (<=3 merge, 4-8 or >8 escalate to Stage 3).
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func hash64(s string) uint64 {
	sum := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint64(sum[:8])
}

// tokenize lowercases and splits on anything that isn't a letter or digit —
// good enough for shingle boundaries; this is not a linguistic tokenizer
// and does not need to be one for a similarity hash.
func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	return fields
}
