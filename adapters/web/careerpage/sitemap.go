package careerpage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// entry is one URL a sitemap (or page links) offered, with its <lastmod>
// normalised to RFC 3339 so entries sort and compare as strings.
type entry struct {
	loc     string
	lastmod string
}

type sitemapDoc struct {
	urls     []entry // from <urlset>
	sitemaps []entry // from <sitemapindex>
}

// parseSitemap decodes a <urlset> or <sitemapindex>, gunzipping first when
// the bytes are gzip. It is namespace-agnostic: real sitemaps carry several
// extension namespaces (xhtml, image) that must not break <loc> extraction.
func parseSitemap(body []byte) (sitemapDoc, error) {
	body, err := gunzipIfNeeded(body)
	if err != nil {
		return sitemapDoc{}, err
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false

	var doc sitemapDoc
	var cur *entry
	var inIndex bool
	var field string
	sawRoot := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return sitemapDoc{}, fmt.Errorf("decode xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "urlset":
				sawRoot = true
			case "sitemapindex":
				sawRoot, inIndex = true, true
			case "url", "sitemap":
				cur = &entry{}
			case "loc", "lastmod":
				field = t.Name.Local
			}
		case xml.CharData:
			if cur != nil && field != "" {
				v := strings.TrimSpace(string(t))
				if field == "loc" {
					cur.loc += v
				} else if lm := parseTime(v); lm != nil {
					cur.lastmod = lm.UTC().Format(time.RFC3339)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "loc", "lastmod":
				field = ""
			case "url", "sitemap":
				if cur != nil && cur.loc != "" {
					if inIndex {
						doc.sitemaps = append(doc.sitemaps, *cur)
					} else {
						doc.urls = append(doc.urls, *cur)
					}
				}
				cur = nil
			}
		}
	}
	if !sawRoot {
		return sitemapDoc{}, fmt.Errorf("not a sitemap: no <urlset> or <sitemapindex> root")
	}
	return doc, nil
}

// linkEntries returns the page's <a href> targets resolved against base, in
// document order. Fragments are dropped so #apply anchors do not multiply
// one job into several URLs.
func linkEntries(body []byte, base string) []entry {
	bu, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var out []entry
	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "a" || !hasAttr {
				continue
			}
			for {
				k, v, more := z.TagAttr()
				if string(k) == "href" {
					if ref, perr := url.Parse(strings.TrimSpace(string(v))); perr == nil {
						abs := bu.ResolveReference(ref)
						abs.Fragment = ""
						out = append(out, entry{loc: abs.String()})
					}
				}
				if !more {
					break
				}
			}
		}
	}
}
