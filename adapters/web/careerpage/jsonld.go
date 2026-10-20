package careerpage

import (
	"bytes"
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"strings"

	htmlparse "golang.org/x/net/html"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/schema"
)

// extractJSONLD returns the raw text of every <script type="application/ld+json">
// block in doc, in document order. It uses the HTML tokenizer rather than a
// regexp so script bodies containing "</" sequences, comments, or attribute
// quoting variations do not derail extraction.
func extractJSONLD(doc []byte) []json.RawMessage {
	var out []json.RawMessage
	z := htmlparse.NewTokenizer(bytes.NewReader(doc))
	for {
		switch z.Next() {
		case htmlparse.ErrorToken:
			return out
		case htmlparse.StartTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "script" || !hasAttr {
				continue
			}
			isLD := false
			for {
				k, v, more := z.TagAttr()
				if string(k) == "type" && strings.EqualFold(strings.TrimSpace(string(v)), "application/ld+json") {
					isLD = true
				}
				if !more {
					break
				}
			}
			if !isLD {
				continue
			}
			if z.Next() == htmlparse.TextToken {
				if txt := bytes.TrimSpace(z.Text()); len(txt) > 0 {
					out = append(out, json.RawMessage(append([]byte(nil), txt...)))
				}
			}
		}
	}
}

func hasJobPosting(blocks []json.RawMessage) bool {
	for _, b := range blocks {
		if bytes.Contains(b, []byte("JobPosting")) {
			return true
		}
	}
	return false
}

func jobBlocks(blocks []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, b := range blocks {
		if bytes.Contains(b, []byte("JobPosting")) {
			out = append(out, b)
		}
	}
	return out
}

// decodeLenient decodes one JSON-LD block. Real sites routinely emit raw
// newlines and tabs inside string values, which strict JSON forbids; those
// are replaced by spaces on a second attempt (outside strings they are legal
// whitespace, so the replacement is safe everywhere).
func decodeLenient(b json.RawMessage) (any, bool) {
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		return v, true
	}
	cleaned := bytes.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, b)
	if err := json.Unmarshal(cleaned, &v); err == nil {
		return v, true
	}
	return nil, false
}

type obj = map[string]any

// findJobPostings walks a decoded JSON-LD document — a single object, an
// array, an @graph, or an ItemList wrapping postings — and returns every
// node typed JobPosting.
func findJobPostings(v any) []obj {
	switch t := v.(type) {
	case []any:
		var out []obj
		for _, e := range t {
			out = append(out, findJobPostings(e)...)
		}
		return out
	case obj:
		if hasType(t, "JobPosting") {
			return []obj{t}
		}
		var out []obj
		for _, key := range []string{"@graph", "itemListElement", "item", "mainEntity", "hasPart"} {
			if child, ok := t[key]; ok {
				out = append(out, findJobPostings(child)...)
			}
		}
		return out
	}
	return nil
}

func hasType(o obj, want string) bool {
	switch t := o["@type"].(type) {
	case string:
		return strings.EqualFold(t[strings.LastIndex(t, "/")+1:], want)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && strings.EqualFold(s[strings.LastIndex(s, "/")+1:], want) {
				return true
			}
		}
	}
	return false
}

func toPosting(jp obj, pg bundlePage, cfg config, kind adapter.SourceKind, raw *adapter.RawResponse) schema.Posting {
	pageURL := pg.URL
	postURL := str(jp["url"])
	if postURL == "" {
		postURL = pageURL
	} else if base, err := url.Parse(pageURL); err == nil {
		if ref, err := url.Parse(postURL); err == nil {
			postURL = base.ResolveReference(ref).String()
		}
	}

	p := schema.Posting{
		ExternalID:          identifier(jp["identifier"]),
		URL:                 postURL,
		ApplyURL:            postURL,
		TitleRaw:            strings.Join(strings.Fields(html.UnescapeString(str(jp["title"]))), " "),
		DescriptionHTML:     descriptionHTML(str(jp["description"])),
		RequirementsText:    strings.TrimSpace(html.UnescapeString(firstStr(jp["qualifications"], jp["skills"], jp["experienceRequirements"]))),
		Department:          strings.TrimSpace(html.UnescapeString(firstStr(jp["occupationalCategory"], jp["industry"]))),
		EmploymentTypeRaw:   strings.Join(strList(jp["employmentType"]), ", "),
		LocationRaw:         locations(jp["jobLocation"]),
		RemoteHint:          strings.EqualFold(str(jp["jobLocationType"]), "TELECOMMUTE"),
		CompensationRawText: compensationText(jp["baseSalary"]),
		Adapter:             string(kind),
		FetchedAt:           raw.FetchedAt,
	}
	if p.ExternalID == "" {
		p.ExternalID = pageURL // the canonical page is the only stable handle left
	}
	if org, ok := jp["hiringOrganization"]; ok {
		p.CompanyNameRaw, p.DomainHint = organization(org)
	}
	if p.CompanyNameRaw == "" {
		p.CompanyNameRaw = cfg.companyName
	}

	if t := parseTime(str(jp["datePosted"])); t != nil {
		p.PostedAt = t
	} else if t := parseTime(pg.LastMod); t != nil {
		p.PostedAt, p.PostedAtEstimated = t, true
	}
	if t := parseTime(str(jp["validThrough"])); t != nil {
		p.DeadlineAt = t
	}
	return p
}

// descriptionHTML returns the description as HTML. Many sites entity-encode
// the markup inside the JSON string ("&lt;p&gt;..."), which would otherwise
// reach the normalizer as literal text.
func descriptionHTML(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "&lt;") && !strings.Contains(s, "<") {
		return html.UnescapeString(s)
	}
	return s
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case obj:
		return firstStr(t["name"], t["@id"], t["value"])
	}
	return ""
}

func firstStr(vs ...any) string {
	for _, v := range vs {
		if s := strings.TrimSpace(str(v)); s != "" {
			return s
		}
	}
	return ""
}

func strList(v any) []string {
	switch t := v.(type) {
	case []any:
		var out []string
		for _, e := range t {
			if s := strings.TrimSpace(str(e)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(str(v)); s != "" {
			return []string{s}
		}
	}
	return nil
}

// identifier handles both the bare form and PropertyValue {name, value}.
func identifier(v any) string {
	if o, ok := v.(obj); ok {
		return strings.TrimSpace(str(o["value"]))
	}
	return strings.TrimSpace(str(v))
}

func organization(v any) (name, domain string) {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(html.UnescapeString(t)), ""
	case obj:
		name = strings.TrimSpace(html.UnescapeString(str(t["name"])))
		for _, key := range []string{"sameAs", "url"} {
			if u, err := url.Parse(str(t[key])); err == nil && u.Hostname() != "" {
				return name, strings.TrimPrefix(u.Hostname(), "www.")
			}
		}
	}
	return name, ""
}

// locations renders each jobLocation as "city, region, country" and joins
// several with "; ". The tiering is the normalizer's job — this only keeps
// what the page says, in a stable order.
func locations(v any) string {
	var items []any
	switch t := v.(type) {
	case []any:
		items = t
	case nil:
		return ""
	default:
		items = []any{t}
	}
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		var parts []string
		switch loc := it.(type) {
		case string:
			parts = []string{loc}
		case obj:
			switch addr := loc["address"].(type) {
			case string:
				parts = []string{addr}
			case obj:
				for _, k := range []string{"addressLocality", "addressRegion", "addressCountry"} {
					if s := strings.TrimSpace(str(addr[k])); s != "" {
						parts = append(parts, s)
					}
				}
			default:
				parts = []string{str(loc["name"])}
			}
		}
		s := strings.TrimSpace(html.UnescapeString(strings.Join(nonEmpty(parts), ", ")))
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, "; ")
}

func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// compensationText renders baseSalary as a snippet the normalizer's
// compensation parser (docs/07 section 7) can read: free text such as
// "8-12 LPA" or "₹ 1,00,000 per month" passes through untouched, and a
// MonetaryAmount becomes "INR 800000 - 1200000 per year". Numbers are never
// rescaled here — LPA/lakh/crore interpretation belongs to one place, the
// normalizer, so the two cannot disagree.
func compensationText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(html.UnescapeString(t))
	case obj:
		currency := strings.TrimSpace(str(t["currency"]))
		val := t["value"]
		var unit, lo, hi string
		switch vv := val.(type) {
		case obj:
			lo, hi = num(vv["minValue"]), num(vv["maxValue"])
			if single := num(vv["value"]); single != "" && lo == "" && hi == "" {
				lo = single
			}
			unit = unitWord(str(vv["unitText"]))
		default:
			lo = num(val)
			unit = unitWord(str(t["unitText"]))
		}
		if lo == "" && hi == "" {
			return ""
		}
		var b strings.Builder
		if currency != "" {
			b.WriteString(currency + " ")
		}
		switch {
		case lo != "" && hi != "" && lo != hi:
			b.WriteString(lo + " - " + hi)
		case lo != "":
			b.WriteString(lo)
		default:
			b.WriteString(hi)
		}
		if unit != "" {
			b.WriteString(" per " + unit)
		}
		return b.String()
	}
	return ""
}

// num prints a JSON number without exponent or trailing zeros, or passes a
// numeric string through (sites quote their numbers often enough).
func num(v any) string {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return ""
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

func unitWord(u string) string {
	switch strings.ToUpper(strings.TrimSpace(u)) {
	case "YEAR", "ANNUAL", "ANNUM":
		return "year"
	case "MONTH":
		return "month"
	case "WEEK":
		return "week"
	case "HOUR":
		return "hour"
	case "DAY":
		return "day"
	}
	return ""
}
