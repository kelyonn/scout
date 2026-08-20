package emailalert

import "regexp"

// indeedProvider follows linkedinProvider's exact shape and carries the
// identical calibration caveat — **unverified against a live alert
// email**; see that file's comment for the full reasoning.
var indeedProvider = regexProvider{
	name:          "indeed",
	senderPattern: regexp.MustCompile(`(?i)@(alert|indeedapply)\.indeed\.com`),
	// Matches Indeed's two known link shapes: the redirect-tracked
	// /rc/clk form alert emails commonly use, and the direct
	// /viewjob?jk={id} form.
	linkPattern: regexp.MustCompile(`(?is)<a[^>]+href="(https://[^"]*indeed\.com/(?:rc/clk|viewjob)[^"]*)"[^>]*>(.*?)</a>`),
}
