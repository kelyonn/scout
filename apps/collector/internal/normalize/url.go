// Package normalize turns adapter output (packages/schema.Posting) into the
// canonical fields packages/schema.NormalizedJob needs: URL and title
// canonicalization, location parsing against the curated gazetteer, and
// compensation parsing. See docs/07-normalization-taxonomy.md.
//
// Redirect resolution (docs/07 section 3, "shortened and tracked links...
// followed up to 3 hops") is out of scope here — it needs a live SSRF-safe
// fetch before canonicalization even starts, and none of the seeded
// Greenhouse boards go through a redirector. CanonicalizeURL operates on
// the URL as given.
package normalize

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
)

// trackingParams and sessionParams are docs/07 section 3, steps 4-5,
// verbatim.
var trackingParamPrefixes = []string{"utm_"}

var trackingParams = map[string]bool{
	"gh_src": true, "lever-source": true, "ref": true, "source": true,
	"src": true, "fbclid": true, "gclid": true, "mc_cid": true, "mc_eid": true,
	"trk": true, "trackingid": true, "refid": true, "position": true,
	"pagenum": true, "li_fat_id": true, "_hsenc": true, "_hsmi": true,
}

var sessionParams = map[string]bool{
	"sessionid": true, "jsessionid": true, "phpsessid": true, "sid": true,
}

// fragmentHosts keeps the URL fragment for hosts that use hash routing —
// docs/07 section 3's per-host override for Workday and SuccessFactors.
// None of the seeded Greenhouse boards need an entry; this exists so a
// later Workday adapter (P3) drops the fragment into the right place
// instead of a new special case.
var fragmentHosts = map[string]bool{
	"myworkdayjobs.com":  true,
	"successfactors.com": true,
}

// CanonicalizeURL implements docs/07 section 3, SCOUT-NORM-001, minus
// redirect resolution (see the package comment). Returns the canonical URL
// and its sha256 hash.
func CanonicalizeURL(raw string) (canonical string, hash [32]byte, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", hash, fmt.Errorf("normalize: parse url: %w", err)
	}

	// 1. Lowercase scheme and host; preserve path case (ATS tokens are
	// sometimes case-sensitive).
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// 2. Force https. "Where the host supports it" needs a live probe this
	// package deliberately doesn't make (see the package comment); every ATS
	// board this pipeline targets serves https, so this is unconditional.
	if u.Scheme == "http" {
		u.Scheme = "https"
	}

	// 3. Strip default ports.
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
	}

	// 4-7. Tracking/session params stripped, remaining sorted, empties
	// dropped.
	if u.RawQuery != "" {
		q := u.Query()
		for key := range q {
			lower := strings.ToLower(key)
			if sessionParams[lower] || trackingParams[lower] || hasTrackingPrefix(lower) {
				q.Del(key)
				continue
			}
			// Drop empty-valued params entirely (step 7); a param with a
			// real value is kept as-is.
			vals := q[key]
			nonEmpty := vals[:0]
			for _, v := range vals {
				if v != "" {
					nonEmpty = append(nonEmpty, v)
				}
			}
			if len(nonEmpty) == 0 {
				q.Del(key)
			} else {
				q[key] = nonEmpty
			}
		}
		// 6. url.Values.Encode() sorts by key, which is step 6's requirement.
		u.RawQuery = q.Encode()
	}

	// 8. Drop the fragment unless this host uses hash routing.
	if !isFragmentHost(u.Hostname()) {
		u.Fragment = ""
	}

	// 9. Drop a trailing slash unless the path is exactly "/".
	if len(u.Path) > 1 && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}

	// 10. url.URL.String() re-escapes with Go's own encoder, which emits
	// uppercase percent-hex and leaves RFC 3986 unreserved characters
	// unescaped — satisfying step 10 as a side effect of round-tripping
	// through net/url rather than needing a separate pass.
	canonical = u.String()
	return canonical, sha256.Sum256([]byte(canonical)), nil
}

func hasTrackingPrefix(lowerKey string) bool {
	for _, prefix := range trackingParamPrefixes {
		if strings.HasPrefix(lowerKey, prefix) {
			return true
		}
	}
	return false
}

func isFragmentHost(host string) bool {
	for suffix := range fragmentHosts {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
