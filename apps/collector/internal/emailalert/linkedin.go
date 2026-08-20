package emailalert

import "regexp"

// linkedinProvider is the reference configuration every other provider in
// this package (indeed.go, glassdoor.go, handshake.go) follows — see
// regexProvider for the shared extraction logic and the package comment
// for the overall design.
//
// **Unverified against a live alert email**, like adapters/ats/teamtailor
// and adapters/ats/workday: LinkedIn's job-alert HTML template is not
// publicly documented the way an ATS's JSON API is, so linkedinJobLink's
// pattern and the "title anchor, then two text runs for company/location"
// extraction shape are built from publicly known characteristics of these
// emails (the `jobs/view/{id}` link path, `trackingId` query parameter, a
// title/company/location block repeated per job) rather than a captured
// real message. Recalibrate against a real subscribed alert before
// trusting this the way the ATS adapters already are — the fixtures here
// model the assumed shape, not a verified one. Every other provider in
// this package inherits the same caveat.
var linkedinProvider = regexProvider{
	name: "linkedin",
	// jobs-noreply@ and jobalerts-noreply@ are LinkedIn's two documented
	// job-alert sending addresses.
	senderPattern: regexp.MustCompile(`(?i)@(jobs-noreply|jobalerts-noreply)\.linkedin\.com`),
	// `comm/` is LinkedIn's own click-redirector path prefix, present on
	// some but not all alert formats, hence the optional group.
	linkPattern: regexp.MustCompile(`(?is)<a[^>]+href="(https://[^"]*linkedin\.com/(?:comm/)?jobs/view/\d+[^"]*)"[^>]*>(.*?)</a>`),
}
