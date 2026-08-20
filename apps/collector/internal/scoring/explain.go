// explain.go — docs/09-ranking-scoring.md section 6's deterministic
// fallback explanation. Built from exactly the inputs Compute already
// produced, so it costs nothing beyond string formatting and is always
// available synchronously — which is what lets a notification go out
// immediately with this text and get upgraded to an LLM-generated one
// later by packages/queue's TaskExplain, per section 7: "a notification
// can be sent with the deterministic explanation and upgraded when the
// LLM one arrives."
package scoring

import (
	"fmt"
	"math"
	"strings"

	"github.com/kelyon/scout/packages/schema"
)

// ExplanationModelTemplate is job_score.explanation_model's value for this
// fallback — distinct from an actual model name (e.g. "qwen2.5:3b-instruct")
// so a caller can tell at a glance whether a row's explanation was ever
// upgraded by the LLM tier.
const ExplanationModelTemplate = "template"

// maxSkillsShown caps the skill list in the explanation text — docs/09's
// own example ("Go, Rust, Kubernetes, …") truncates rather than listing
// every match, and a job with a long skill list would otherwise produce
// an explanation longer than the LLM-generated one it stands in for.
const maxSkillsShown = 5

// Explain builds the deterministic fallback from job and the Result
// Compute already returned for it — no I/O, no LLM call.
func Explain(job schema.NormalizedJob, r Result) string {
	parts := make([]string, 0, 3)

	if line := skillMatchLine(r.ScoreInputs); line != "" {
		parts = append(parts, line)
	}
	if line := locationLine(job); line != "" {
		parts = append(parts, line)
	}
	if line := compensationLine(job, r.ScoreInputs); line != "" {
		parts = append(parts, line)
	}

	if len(parts) == 0 {
		// Every input this template draws on was placeholder/unknown —
		// rare (it requires an empty skill list, an unresolved location,
		// and no compensation data all at once) but not impossible, and a
		// job_score row's explanation is TEXT NULL-able for exactly this
		// case rather than a misleadingly specific empty string.
		return "Not enough data yet for a specific explanation — check back after this posting is re-processed."
	}
	return strings.Join(parts, " ")
}

func skillMatchLine(inputs map[string]any) string {
	skillInputs, ok := inputs["skill_match"].(map[string]any)
	if !ok {
		return ""
	}
	jobSkills, _ := skillInputs["job_skills"].([]string)
	missing, _ := skillInputs["missing_skills"].([]string)
	if len(jobSkills) == 0 {
		return ""
	}

	missingSet := make(map[string]bool, len(missing))
	for _, s := range missing {
		missingSet[s] = true
	}
	matched := make([]string, 0, len(jobSkills))
	for _, s := range jobSkills {
		if !missingSet[s] {
			matched = append(matched, s)
		}
	}

	if len(matched) == 0 {
		return fmt.Sprintf("Matches 0 of %d required skills.", len(jobSkills))
	}

	shown := matched
	var suffix string
	if len(shown) > maxSkillsShown {
		shown = shown[:maxSkillsShown]
		suffix = ", …)."
	} else {
		suffix = ")."
	}
	return fmt.Sprintf("Matches %d of %d required skills (%s%s", len(matched), len(jobSkills), strings.Join(shown, ", "), suffix)
}

var locationTierNames = map[int16]string{1: "Tier 1", 2: "Tier 2", 3: "Tier 3", 4: "Tier 4"}

func locationLine(job schema.NormalizedJob) string {
	switch {
	case job.WorkMode == schema.WorkRemote && job.LocationCity == "":
		return "Remote."
	case job.LocationCity == "":
		return ""
	case locationTierNames[job.LocationTier] != "":
		return fmt.Sprintf("%s location: %s.", locationTierNames[job.LocationTier], job.LocationCity)
	default:
		return job.LocationCity + "."
	}
}

func compensationLine(job schema.NormalizedJob, inputs map[string]any) string {
	if job.CompNormalizedINRMonth == nil {
		return ""
	}
	compInputs, ok := inputs["compensation"].(map[string]any)
	if !ok {
		return ""
	}
	confidenceLow, _ := compInputs["confidence_low"].(bool)
	if confidenceLow {
		// A stated figure exists but there weren't enough comparables to
		// rank it — still worth stating the raw number, just without a
		// percentile claim we can't back up.
		return fmt.Sprintf("₹%s/month.", formatThousands(*job.CompNormalizedINRMonth))
	}
	percentile, _ := compInputs["percentile"].(float64)
	return fmt.Sprintf(
		"₹%s/month, %s percentile among comparable roles.",
		formatThousands(*job.CompNormalizedINRMonth), ordinal(int(math.Round(percentile*100))),
	)
}

// formatThousands renders 85000 as "85,000" — display formatting only,
// not the Indian-numbering parse path (packages/normalize handles parsing
// "8 LPA"/"1,00,000" from source text; this only ever formats a number
// this system already computed).
func formatThousands(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func ordinal(n int) string {
	if n%100 >= 11 && n%100 <= 13 {
		return fmt.Sprintf("%dth", n)
	}
	switch n % 10 {
	case 1:
		return fmt.Sprintf("%dst", n)
	case 2:
		return fmt.Sprintf("%dnd", n)
	case 3:
		return fmt.Sprintf("%drd", n)
	default:
		return fmt.Sprintf("%dth", n)
	}
}
