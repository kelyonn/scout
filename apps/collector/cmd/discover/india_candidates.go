package main

import (
	"github.com/kelyon/scout/apps/collector/internal/discovery"
	padapter "github.com/kelyon/scout/packages/adapter"
)

// indiaCandidates is a hand-curated list of Indian startups, mid-size
// companies, and India-heavy engineering offices of larger companies —
// unlike candidateListURLs' three files (an unfiltered global company
// list from every country), this exists because that global list has no
// location signal at all, so a run against it alone skews overwhelmingly
// non-India: verified live 2026-08-24, 1,849 of the jobs pulled from that
// night's 2,053 newly-discovered companies were non-India versus 86
// India-located. There is no free, public, ToS-compliant API for the
// platforms that actually dominate Indian hiring (Naukri, LinkedIn,
// Internshala all require paid/partner access — checked, not assumed,
// and AGENTS.md rule 10 rules them out either way) — this list is the
// realistic alternative: real companies known to have real Indian
// engineering presence, checked against the same three ATS platforms
// this project already supports.
//
// Company -> platform mapping is a best guess, not verified in advance —
// that verification is exactly what discovery.Assessor does live for
// every entry below, the same as every other candidate this tool
// assesses. A wrong guess (wrong slug, wrong platform, or a company that
// has since moved off the guessed platform) costs one harmless HTTP
// request and is silently not inserted — same "resolves or it doesn't"
// posture as the URL-fetched lists. Slugs follow each platform's own
// convention (Greenhouse/Ashby: lowercase, no separators; Lever: often
// hyphenated) — where genuinely unsure which platform a company uses,
// it's listed against more than one.
//
// This list will go stale (companies migrate ATS platforms, get
// acquired, shut down) exactly like docs/05's own GCC list does — it is
// a snapshot, not a promise, and re-running this tool later will not
// re-discover anything already in `company`, so entries that fail here
// are not automatically retried.
var indiaCandidates = []discovery.Candidate{
	// Fintech
	{Slug: "razorpay", Kind: padapter.SourceKindGreenhouse},
	{Slug: "cred-club", Kind: padapter.SourceKindLever},
	{Slug: "cred", Kind: padapter.SourceKindGreenhouse},
	{Slug: "groww", Kind: padapter.SourceKindGreenhouse},
	{Slug: "phonepe", Kind: padapter.SourceKindGreenhouse},
	{Slug: "cashfree", Kind: padapter.SourceKindGreenhouse},
	{Slug: "juspay", Kind: padapter.SourceKindGreenhouse},
	{Slug: "m2pfintech", Kind: padapter.SourceKindGreenhouse},
	{Slug: "slice", Kind: padapter.SourceKindGreenhouse},
	{Slug: "sliceit", Kind: padapter.SourceKindLever},
	{Slug: "kreditbee", Kind: padapter.SourceKindGreenhouse},
	{Slug: "getsimpl", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zeta", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zetasuite", Kind: padapter.SourceKindLever},
	{Slug: "coindcx", Kind: padapter.SourceKindGreenhouse},
	{Slug: "coinswitch", Kind: padapter.SourceKindGreenhouse},
	{Slug: "bharatpe", Kind: padapter.SourceKindGreenhouse},
	{Slug: "jupiter", Kind: padapter.SourceKindGreenhouse},
	{Slug: "jupitermoney", Kind: padapter.SourceKindLever},
	{Slug: "fi", Kind: padapter.SourceKindGreenhouse},
	{Slug: "epifi", Kind: padapter.SourceKindLever},
	{Slug: "setu", Kind: padapter.SourceKindGreenhouse},
	{Slug: "kaleidofin", Kind: padapter.SourceKindGreenhouse},
	{Slug: "khatabook", Kind: padapter.SourceKindGreenhouse},
	{Slug: "digit", Kind: padapter.SourceKindGreenhouse},
	{Slug: "godigit", Kind: padapter.SourceKindLever},
	{Slug: "acko", Kind: padapter.SourceKindGreenhouse},
	{Slug: "ackoinsurance", Kind: padapter.SourceKindLever},
	{Slug: "smallcase", Kind: padapter.SourceKindGreenhouse},
	{Slug: "indmoney", Kind: padapter.SourceKindGreenhouse},
	{Slug: "yubi", Kind: padapter.SourceKindGreenhouse},
	{Slug: "signzy", Kind: padapter.SourceKindGreenhouse},
	{Slug: "increff", Kind: padapter.SourceKindGreenhouse},
	{Slug: "vahan", Kind: padapter.SourceKindGreenhouse},
	{Slug: "recko", Kind: padapter.SourceKindGreenhouse},

	// B2B SaaS
	{Slug: "chargebee", Kind: padapter.SourceKindGreenhouse},
	{Slug: "freshworks", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zoho", Kind: padapter.SourceKindGreenhouse},
	{Slug: "whatfix", Kind: padapter.SourceKindGreenhouse},
	{Slug: "moengage", Kind: padapter.SourceKindGreenhouse},
	{Slug: "clevertap", Kind: padapter.SourceKindGreenhouse},
	{Slug: "yellowai", Kind: padapter.SourceKindGreenhouse},
	{Slug: "yellow-ai", Kind: padapter.SourceKindLever},
	{Slug: "uniphore", Kind: padapter.SourceKindGreenhouse},
	{Slug: "icertis", Kind: padapter.SourceKindGreenhouse},
	{Slug: "druva", Kind: padapter.SourceKindGreenhouse},
	{Slug: "gupshup", Kind: padapter.SourceKindGreenhouse},
	{Slug: "exotel", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zluri", Kind: padapter.SourceKindGreenhouse},
	{Slug: "leadsquared", Kind: padapter.SourceKindGreenhouse},
	{Slug: "rocketlane", Kind: padapter.SourceKindGreenhouse},
	{Slug: "hasura", Kind: padapter.SourceKindGreenhouse},
	{Slug: "hevodata", Kind: padapter.SourceKindGreenhouse},
	{Slug: "hevo", Kind: padapter.SourceKindLever},
	{Slug: "wingify", Kind: padapter.SourceKindGreenhouse},
	{Slug: "innovaccer", Kind: padapter.SourceKindGreenhouse},
	{Slug: "thoughtspot", Kind: padapter.SourceKindGreenhouse},
	{Slug: "fractal", Kind: padapter.SourceKindGreenhouse},
	{Slug: "musigma", Kind: padapter.SourceKindGreenhouse},
	{Slug: "highspot", Kind: padapter.SourceKindGreenhouse},
	{Slug: "clari", Kind: padapter.SourceKindGreenhouse},
	{Slug: "branchmetrics", Kind: padapter.SourceKindGreenhouse},
	{Slug: "airmeet", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zluri-hq", Kind: padapter.SourceKindLever},
	{Slug: "darwinbox", Kind: padapter.SourceKindGreenhouse},
	{Slug: "draup", Kind: padapter.SourceKindGreenhouse},
	{Slug: "locus", Kind: padapter.SourceKindGreenhouse},
	{Slug: "shipsy", Kind: padapter.SourceKindGreenhouse},
	{Slug: "moglix", Kind: padapter.SourceKindGreenhouse},
	{Slug: "postman", Kind: padapter.SourceKindGreenhouse},
	{Slug: "browserstack", Kind: padapter.SourceKindGreenhouse},

	// Consumer / commerce
	{Slug: "meesho", Kind: padapter.SourceKindGreenhouse},
	{Slug: "urbancompany", Kind: padapter.SourceKindGreenhouse},
	{Slug: "urban-company", Kind: padapter.SourceKindLever},
	{Slug: "sharechat", Kind: padapter.SourceKindGreenhouse},
	{Slug: "moj", Kind: padapter.SourceKindGreenhouse},
	{Slug: "dream11", Kind: padapter.SourceKindGreenhouse},
	{Slug: "mpl", Kind: padapter.SourceKindGreenhouse},
	{Slug: "games24x7", Kind: padapter.SourceKindGreenhouse},
	{Slug: "pharmeasy", Kind: padapter.SourceKindGreenhouse},
	{Slug: "netmeds", Kind: padapter.SourceKindGreenhouse},
	{Slug: "nykaa", Kind: padapter.SourceKindGreenhouse},
	{Slug: "lenskart", Kind: padapter.SourceKindGreenhouse},
	{Slug: "curefit", Kind: padapter.SourceKindGreenhouse},
	{Slug: "cure-fit", Kind: padapter.SourceKindLever},
	{Slug: "licious", Kind: padapter.SourceKindGreenhouse},
	{Slug: "rebelfoods", Kind: padapter.SourceKindGreenhouse},
	{Slug: "spinny", Kind: padapter.SourceKindGreenhouse},
	{Slug: "cars24", Kind: padapter.SourceKindGreenhouse},
	{Slug: "cardekho", Kind: padapter.SourceKindGreenhouse},
	{Slug: "porter", Kind: padapter.SourceKindGreenhouse},
	{Slug: "rapido", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zepto", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zeptonow", Kind: padapter.SourceKindLever},
	{Slug: "blinkit", Kind: padapter.SourceKindGreenhouse},
	{Slug: "swiggy", Kind: padapter.SourceKindGreenhouse},
	{Slug: "vedantu", Kind: padapter.SourceKindGreenhouse},
	{Slug: "unacademy", Kind: padapter.SourceKindGreenhouse},
	{Slug: "upgrad", Kind: padapter.SourceKindGreenhouse},
	{Slug: "byjus", Kind: padapter.SourceKindGreenhouse},
	{Slug: "practo", Kind: padapter.SourceKindGreenhouse},
	{Slug: "1mg", Kind: padapter.SourceKindGreenhouse},
	{Slug: "tata1mg", Kind: padapter.SourceKindLever},
	{Slug: "healthifyme", Kind: padapter.SourceKindGreenhouse},
	{Slug: "mfine", Kind: padapter.SourceKindGreenhouse},

	// Logistics / B2B commerce
	{Slug: "delhivery", Kind: padapter.SourceKindGreenhouse},
	{Slug: "blackbuck", Kind: padapter.SourceKindGreenhouse},
	{Slug: "zetwerk", Kind: padapter.SourceKindGreenhouse},
	{Slug: "infra-market", Kind: padapter.SourceKindLever},
	{Slug: "ofbusiness", Kind: padapter.SourceKindGreenhouse},
	{Slug: "udaan", Kind: padapter.SourceKindGreenhouse},
	{Slug: "moglix", Kind: padapter.SourceKindLever},

	// Deep tech / dev tools
	{Slug: "rubrik", Kind: padapter.SourceKindGreenhouse},
	{Slug: "druva", Kind: padapter.SourceKindLever},
	{Slug: "zohocorp", Kind: padapter.SourceKindGreenhouse},
	{Slug: "haptik", Kind: padapter.SourceKindGreenhouse},
	{Slug: "observeai", Kind: padapter.SourceKindGreenhouse},
	{Slug: "observe-ai", Kind: padapter.SourceKindLever},
	{Slug: "hyperverge", Kind: padapter.SourceKindGreenhouse},
	{Slug: "hyperface", Kind: padapter.SourceKindGreenhouse},
	{Slug: "turing", Kind: padapter.SourceKindGreenhouse},
	{Slug: "sprinklr", Kind: padapter.SourceKindGreenhouse},

	// Ashby-hosted (skews younger/startup)
	{Slug: "cred", Kind: padapter.SourceKindAshby},
	{Slug: "groww", Kind: padapter.SourceKindAshby},
	{Slug: "zepto", Kind: padapter.SourceKindAshby},
	{Slug: "rocketlane", Kind: padapter.SourceKindAshby},
	{Slug: "hasura", Kind: padapter.SourceKindAshby},
	{Slug: "setu", Kind: padapter.SourceKindAshby},
	{Slug: "turing", Kind: padapter.SourceKindAshby},
	{Slug: "vahan", Kind: padapter.SourceKindAshby},
	{Slug: "jupiter", Kind: padapter.SourceKindAshby},
	{Slug: "recko", Kind: padapter.SourceKindAshby},
	{Slug: "zluri", Kind: padapter.SourceKindAshby},
	{Slug: "airmeet", Kind: padapter.SourceKindAshby},
}
