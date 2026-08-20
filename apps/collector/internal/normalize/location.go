package normalize

import (
	"regexp"
	"strings"

	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

// splitDelimiters implements docs/07 section 6 step 1: "/, ;, |, 'or',
// 'and'".
var splitDelimiters = regexp.MustCompile(`(?i)\s*(?:/|;|\||\bor\b|\band\b)\s*`)

var (
	remotePattern = regexp.MustCompile(`(?i)\bremote\b`)
	hybridPattern = regexp.MustCompile(`(?i)\bhybrid\b`)
)

// visaPositivePattern and visaNegativePattern are docs/07 section 6's last
// paragraph, verbatim signals.
var (
	visaPositivePattern = regexp.MustCompile(`(?i)visa sponsorship available|we sponsor h-?1b|sponsor.{0,20}visa`)
	visaNegativePattern = regexp.MustCompile(`(?i)must have work authorization`)
)

// NormalizedLocation is the result of resolving and tiering a raw location
// string against the gazetteer — docs/07 section 6.
type NormalizedLocation struct {
	City     string
	Region   string
	Country  string
	Tier     int16 // 1-4; 0 means unresolved (no gazetteer match, not remote)
	WorkMode schema.WorkMode
}

// NormalizeLocation implements docs/07 section 6. remoteHint comes from the
// adapter (packages/schema.Posting.RemoteHint) when the source has a
// structured remote flag separate from the free-text location string.
func NormalizeLocation(raw string, remoteHint bool, gaz *taxonomy.Gazetteer) NormalizedLocation {
	mode := detectWorkMode(raw, remoteHint)

	best := NormalizedLocation{WorkMode: mode}
	for _, candidate := range splitDelimiters.Split(raw, -1) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		loc, ok := gaz.Resolve(candidate)
		if !ok {
			continue
		}
		tier := tierFor(loc, mode)
		// Step 6: job tier is the BEST (lowest number) among candidates.
		if best.Tier == 0 || tier < best.Tier {
			best = NormalizedLocation{
				City:     loc.Canonical,
				Region:   loc.Region,
				Country:  loc.Country,
				Tier:     tier,
				WorkMode: mode,
			}
		}
	}

	// Fully remote with no gazetteer-resolved physical tie is still a real,
	// scoreable Tier 3 opportunity ("Any | remote (India-eligible) | 3" and
	// the San Francisco-remote row in docs/07 section 6) — it must not fall
	// through to the unresolved (Tier 0) case just because the raw string
	// was "Remote" with no city in it.
	if best.Tier == 0 && mode == schema.WorkRemote {
		best.Tier = 3
	}

	return best
}

func detectWorkMode(raw string, remoteHint bool) schema.WorkMode {
	if remoteHint || remotePattern.MatchString(raw) {
		return schema.WorkRemote
	}
	if hybridPattern.MatchString(raw) {
		return schema.WorkHybrid
	}
	if raw == "" {
		return schema.WorkUnknown
	}
	// A location string that names neither remote nor hybrid states a
	// physical place; treated as onsite for tiering, per docs/07 section 6's
	// interaction table.
	return schema.WorkOnsite
}

// tierFor is docs/07 section 6's tiering table plus the work-mode
// interaction table, collapsed into one rule: Bengaluru is always Tier 1
// regardless of mode; a non-Bengaluru remote posting is always Tier 3
// regardless of where the company itself sits; everything else follows the
// resolved country. Hybrid is treated identically to onsite — the doc notes
// hybrid carries "a small penalty relative to fully remote at the same
// location" without giving a value, so no penalty is modeled here; a
// documented P1 simplification.
func tierFor(loc taxonomy.Location, mode schema.WorkMode) int16 {
	if loc.IsBengaluru {
		return 1
	}
	if mode == schema.WorkRemote {
		return 3
	}
	if loc.Country == "IN" {
		return 2
	}
	if loc.Country != "" {
		return 4
	}
	return 0
}

// DetectVisaSponsorship implements docs/07 section 6's visa signal, applied
// as a ±0.05 adjustment within Tier 4 by the scoring package — this
// function only extracts the signal, it does not apply the adjustment.
func DetectVisaSponsorship(descriptionText string) *bool {
	positive := visaPositivePattern.MatchString(descriptionText)
	negative := visaNegativePattern.MatchString(descriptionText)
	switch {
	case positive && !negative:
		v := true
		return &v
	case negative && !positive:
		v := false
		return &v
	default:
		return nil
	}
}
