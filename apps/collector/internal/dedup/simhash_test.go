package dedup

import "testing"

func TestSimHash_IdenticalTextsProduceIdenticalHash(t *testing.T) {
	text := "Build backend services in Go and Python for a distributed systems team."
	first := SimHash(text)
	second := SimHash(text)
	if first != second {
		t.Error("SimHash of the same text twice should be identical")
	}
}

// TestSimHash_ByteIdenticalContentDifferentFormattingHasLowHammingDistance
// is what Gate 3's <=3 merge threshold is actually for per docs/08: the
// same content re-fetched, or the same text with trivial whitespace/HTML
// differences — not a paraphrase. Tokenize strips formatting entirely, so
// this really is the same shingle set either way.
func TestSimHash_ByteIdenticalContentDifferentFormattingHasLowHammingDistance(t *testing.T) {
	a := "We are looking for a Software Engineering Intern to join our backend team.\n" +
		"You will build and maintain services in Go and Python that power our distributed\n" +
		"systems platform, working closely with senior engineers on payments infrastructure."
	b := "We are looking for a Software Engineering Intern to join our backend team.   " +
		"You will build and maintain services in Go and Python that power our distributed   " +
		"systems platform, working closely with senior engineers on payments infrastructure.  "

	distance := HammingDistance(SimHash(a), SimHash(b))
	if distance > gate3MergeThreshold {
		t.Errorf("formatting-only difference: hamming distance = %d, want <= %d (gate3MergeThreshold)", distance, gate3MergeThreshold)
	}
}

// TestSimHash_LightlyReworkedTextEscalatesRatherThanMerges is the other
// half of the same design point: text with a handful of genuine word-level
// edits (not just formatting) is exactly what docs/08 section 3.2 sends to
// Stage 3 for semantic confirmation ("distance 4-8 -> escalate"), not what
// Gate 3 merges outright. A few paraphrased words being *outside* the
// merge threshold is Stage 2 working as designed, not a false negative —
// Stage 3's embedding comparison is what actually confirms paraphrases.
func TestSimHash_LightlyReworkedTextEscalatesRatherThanMerges(t *testing.T) {
	a := "We are looking for a Software Engineering Intern to join our backend team. " +
		"You will build and maintain services in Go and Python that power our distributed " +
		"systems platform, working closely with senior engineers on payments infrastructure. " +
		"This role involves writing clean, well-tested code, participating in code reviews, " +
		"and collaborating with product managers to ship features that impact millions of users."
	b := "We are looking for a Software Engineering Intern to join our backend team. " +
		"You will build and maintain services in Go and Python that power our distributed " +
		"systems platform, working closely with senior engineers on payments infra. " +
		"This role involves writing clean, well-tested code, participating in code review, " +
		"and collaborating with product managers to ship features that impact millions of users."

	distance := HammingDistance(SimHash(a), SimHash(b))
	if distance <= gate3MergeThreshold {
		t.Errorf("lightly reworked text: hamming distance = %d, want > %d (gate3MergeThreshold) — should escalate to Stage 3, not auto-merge", distance, gate3MergeThreshold)
	}
}

func TestSimHash_UnrelatedTextsHaveHighHammingDistance(t *testing.T) {
	a := "Build backend services in Go and Python for a distributed systems team working on payments infrastructure and API design."
	b := "Lead our marketing team's social media strategy and coordinate with the design team on brand campaigns for the holiday season."

	distance := HammingDistance(SimHash(a), SimHash(b))
	if distance <= gate3MergeThreshold {
		t.Errorf("unrelated texts: hamming distance = %d, want > %d (gate3MergeThreshold)", distance, gate3MergeThreshold)
	}
}

func TestSimHash_EmptyTextIsZero(t *testing.T) {
	if SimHash("") != 0 {
		t.Errorf("SimHash(\"\") = %d, want 0", SimHash(""))
	}
}

func TestHammingDistance_IdenticalIsZero(t *testing.T) {
	if got := HammingDistance(0xABCD, 0xABCD); got != 0 {
		t.Errorf("HammingDistance(same, same) = %d, want 0", got)
	}
}

func TestHammingDistance_CountsDifferingBits(t *testing.T) {
	// 0b1010 vs 0b0101 differ in all 4 low bits.
	if got := HammingDistance(0b1010, 0b0101); got != 4 {
		t.Errorf("HammingDistance(0b1010, 0b0101) = %d, want 4", got)
	}
}
