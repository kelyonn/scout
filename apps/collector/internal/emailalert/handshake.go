package emailalert

import "regexp"

// handshakeProvider follows linkedinProvider's exact shape and carries
// the identical calibration caveat — **unverified against a live alert
// email**; see that file's comment for the full reasoning. Also the one
// provider docs/14-legal-compliance.md singles out by name: the user's
// Handshake account is tied to their university identity, which is why
// email alerts — never automated access of any kind — are the only
// acceptable path for this source, not just the cheapest one.
var handshakeProvider = regexProvider{
	name:          "handshake",
	senderPattern: regexp.MustCompile(`(?i)@(notifications|handshake)\.joinhandshake\.com`),
	linkPattern:   regexp.MustCompile(`(?is)<a[^>]+href="(https://[^"]*joinhandshake\.com/(?:jobs|job-postings)/\d+[^"]*)"[^>]*>(.*?)</a>`),
}
