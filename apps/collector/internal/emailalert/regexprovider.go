package emailalert

import (
	"html"
	"regexp"
)

// regexProvider is every Provider in this package (linkedin.go, indeed.go,
// glassdoor.go, handshake.go) — each platform's alert HTML differs only in
// its sending domain and job-link URL shape, not in how title/company/
// location are recovered around a matched link, so that logic lives here
// once rather than four times. See linkedin.go's comment for the shared
// calibration caveat every instance of this type carries.
type regexProvider struct {
	name          string
	senderPattern *regexp.Regexp
	// linkPattern must have exactly two capture groups: (1) the href, (2)
	// the anchor's inner HTML (the visible title before tag stripping).
	linkPattern *regexp.Regexp
}

func (p regexProvider) Name() string { return p.name }

func (p regexProvider) Matches(fromHeader string) bool {
	return p.senderPattern.MatchString(fromHeader)
}

// Parse finds every linkPattern match in msg.HTML and pairs it with the
// two plain-text runs immediately following it as company and location —
// see html.go's textRunsAfter for why that heuristic, not a full HTML
// parse, is what this package uses.
func (p regexProvider) Parse(msg Message) ([]ExtractedPosting, error) {
	var postings []ExtractedPosting
	for _, m := range p.linkPattern.FindAllStringSubmatchIndex(msg.HTML, -1) {
		trackingURL := html.UnescapeString(msg.HTML[m[2]:m[3]])
		title := stripTags(msg.HTML[m[4]:m[5]])
		if title == "" {
			continue
		}

		runs := textRunsAfter(msg.HTML, m[1], 400, 2)
		var company, location string
		if len(runs) > 0 {
			company = runs[0]
		}
		if len(runs) > 1 {
			location = runs[1]
		}

		postings = append(postings, ExtractedPosting{
			TitleRaw: title, CompanyNameRaw: company, LocationRaw: location, TrackingURL: trackingURL,
		})
	}
	return postings, nil
}
