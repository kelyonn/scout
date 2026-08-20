package emailalert

import "regexp"

// glassdoorProvider follows linkedinProvider's exact shape and carries
// the identical calibration caveat — **unverified against a live alert
// email**; see that file's comment for the full reasoning.
var glassdoorProvider = regexProvider{
	name:          "glassdoor",
	senderPattern: regexp.MustCompile(`(?i)@(noreply|alerts)\.glassdoor\.com`),
	linkPattern:   regexp.MustCompile(`(?is)<a[^>]+href="(https://[^"]*glassdoor\.co[a-z.]*/(?:job-listing|partner/jobListing\.htm)[^"]*)"[^>]*>(.*?)</a>`),
}
