package dedup

import (
	"strings"
	"testing"
)

const (
	jobA = `We are hiring a Software Engineering Intern for our backend team this summer.

You will work directly with senior engineers on real production systems.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.

We do not accept unsolicited resumes from recruiting or staffing agencies.`

	jobB = `We are hiring a Frontend Engineering Intern for our web platform team this summer.

You will build user-facing features used by millions of people every day.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.

We do not accept unsolicited resumes from recruiting or staffing agencies.`

	jobC = `We are hiring a Data Engineering Intern for our analytics platform this summer.

You will design and build pipelines that process billions of events per day.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.

We do not accept unsolicited resumes from recruiting or staffing agencies.`

	jobD = `We are hiring a Mobile Engineering Intern for our iOS team this summer.

You will ship features to millions of users on the App Store.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.

We do not accept unsolicited resumes from recruiting or staffing agencies.`

	jobE = `We are hiring a Security Engineering Intern for our platform security team this summer.

You will help us find and fix vulnerabilities before attackers do.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.

We do not accept unsolicited resumes from recruiting or staffing agencies.`
)

func TestLearnBoilerplateHashes_BelowColdStartFloorReturnsNil(t *testing.T) {
	// 4 postings — one short of coldStartMinPostings (5).
	hashes := LearnBoilerplateHashes([]string{jobA, jobB, jobC, jobD})
	if hashes != nil {
		t.Errorf("LearnBoilerplateHashes with %d postings = %v, want nil (below cold-start floor)", 4, hashes)
	}
}

func TestLearnBoilerplateHashes_LearnsSharedParagraphAtOrAboveThreshold(t *testing.T) {
	// All 5 postings share the EEO paragraph and the agency-disclaimer
	// paragraph (100% >= 60% threshold); each has a unique intro/body.
	hashes := LearnBoilerplateHashes([]string{jobA, jobB, jobC, jobD, jobE})
	if hashes == nil {
		t.Fatal("LearnBoilerplateHashes with 5 postings = nil, want a learned set")
	}

	eeoHash := paragraphHash("Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.")
	if !hashes[eeoHash] {
		t.Error("EEO paragraph (present in 5/5 postings) should be learned as boilerplate")
	}

	uniqueHash := paragraphHash("You will work directly with senior engineers on real production systems.")
	if hashes[uniqueHash] {
		t.Error("a paragraph unique to one posting should not be learned as boilerplate")
	}
}

func TestStripDescription_RemovesLearnedBoilerplate(t *testing.T) {
	hashes := LearnBoilerplateHashes([]string{jobA, jobB, jobC, jobD, jobE})
	stripped := StripDescription(jobA, hashes)

	if strings.Contains(stripped, "equal opportunity employer") {
		t.Error("learned EEO boilerplate should be removed from the stripped output")
	}
	if !strings.Contains(stripped, "Software Engineering Intern") {
		t.Error("unique content should survive stripping")
	}
}

func TestStripDescription_ColdStartAppliesOnlyGlobalPatterns(t *testing.T) {
	// nil learnedHashes (cold-start: fewer than 5 postings) — only the
	// global regex patterns strip, per docs/08's documented degradation.
	stripped := StripDescription(jobA, nil)

	if strings.Contains(stripped, "equal opportunity employer") {
		t.Error("global EEO pattern should still strip even with no learned per-company set")
	}
	if !strings.Contains(stripped, "Software Engineering Intern") {
		t.Error("unique content should survive stripping")
	}
}

func TestStripDescription_GlobalPatternsStripRecruiterDisclaimer(t *testing.T) {
	stripped := StripDescription(jobA, nil)
	if strings.Contains(stripped, "unsolicited resumes") {
		t.Error("recruiter-agency disclaimer should be stripped by the global pattern list")
	}
}

func TestStripDescription_ShortParagraphsNeverConsidered(t *testing.T) {
	text := "Hi.\n\nWe are hiring a Software Engineering Intern for our backend team this summer, working on real production systems."
	stripped := StripDescription(text, nil)
	if !strings.Contains(stripped, "Software Engineering Intern") {
		t.Error("the real content paragraph should survive")
	}
	// "Hi." is under boilerplateMinParagraphLen (40 chars) and should be
	// dropped by splitParagraphs entirely, not passed through untouched.
	if strings.Contains(stripped, "Hi.") {
		t.Error("a paragraph under the minimum length should not appear in the output")
	}
}
