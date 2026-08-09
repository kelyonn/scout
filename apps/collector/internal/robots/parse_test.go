package robots

import "testing"

func TestAllowed(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
		want bool
	}{
		{
			name: "no groups at all means unrestricted",
			body: "",
			path: "/anything",
			want: true,
		},
		{
			name: "simple disallow",
			body: "User-agent: *\nDisallow: /private",
			path: "/private/resume.pdf",
			want: false,
		},
		{
			name: "disallow does not match an unrelated path",
			body: "User-agent: *\nDisallow: /private",
			path: "/public/jobs",
			want: true,
		},
		{
			name: "empty disallow value permits everything",
			body: "User-agent: *\nDisallow:",
			path: "/private/anything",
			want: true,
		},
		{
			name: "allow overrides a shorter disallow",
			body: "User-agent: *\nDisallow: /private\nAllow: /private/public-notice",
			path: "/private/public-notice/index.html",
			want: true,
		},
		{
			name: "a longer disallow beats a shorter allow",
			body: "User-agent: *\nAllow: /\nDisallow: /admin",
			path: "/admin/panel",
			want: false,
		},
		{
			name: "equal-length allow and disallow: allow wins the tie",
			body: "User-agent: *\nDisallow: /a\nAllow: /a",
			path: "/a",
			want: true,
		},
		{
			name: "the scout group is used instead of the star group when both exist",
			body: "User-agent: *\nDisallow: /\nUser-agent: Scout\nAllow: /",
			path: "/anything",
			want: true,
		},
		{
			name: "scout group matching is case-insensitive",
			body: "User-agent: SCOUT\nAllow: /\nUser-agent: *\nDisallow: /",
			path: "/anything",
			want: true,
		},
		{
			name: "a source with only a star group falls back to it",
			body: "User-agent: *\nDisallow: /blocked",
			path: "/blocked/x",
			want: false,
		},
		{
			name: "an unrelated named group does not apply to us",
			body: "User-agent: Googlebot\nDisallow: /\nUser-agent: *\nAllow: /",
			path: "/anything",
			want: true,
		},
		{
			name: "wildcard mid-pattern",
			body: "User-agent: *\nDisallow: /*.pdf",
			path: "/careers/roles/resume.pdf",
			want: false,
		},
		{
			name: "wildcard does not match without the suffix",
			body: "User-agent: *\nDisallow: /*.pdf",
			path: "/careers/roles/index.html",
			want: true,
		},
		{
			name: "end anchor requires an exact suffix",
			body: "User-agent: *\nDisallow: /page$",
			path: "/page",
			want: false,
		},
		{
			name: "end anchor does not match a longer path",
			body: "User-agent: *\nDisallow: /page$",
			path: "/pages/index.html",
			want: true,
		},
		{
			name: "consecutive user-agent lines share one rule set",
			body: "User-agent: Googlebot\nUser-agent: Scout\nDisallow: /shared\n",
			path: "/shared/x",
			want: false,
		},
		{
			name: "rules before any user-agent line are ignored",
			body: "Disallow: /orphan\nUser-agent: *\nAllow: /\n",
			path: "/orphan",
			want: true,
		},
		{
			name: "comments and blank lines are ignored",
			body: "# comment\n\nUser-agent: *\n# another comment\nDisallow: /private # trailing\n",
			path: "/private/x",
			want: false,
		},
		{
			name: "malformed lines are skipped without aborting the parse",
			body: "not a valid line\nUser-agent: *\nDisallow: /private\n",
			path: "/private/x",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Parse([]byte(tt.body))
			if got := r.Allowed(tt.path); got != tt.want {
				t.Errorf("Allowed(%q) = %v, want %v\nbody:\n%s", tt.path, got, tt.want, tt.body)
			}
		})
	}
}

func TestCrawlDelay(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *float64
	}{
		{
			name: "no crawl-delay declared",
			body: "User-agent: *\nDisallow: /private",
			want: nil,
		},
		{
			name: "crawl-delay on the applicable group",
			body: "User-agent: *\nCrawl-delay: 10\nDisallow: /private",
			want: floatPtr(10),
		},
		{
			name: "crawl-delay on the scout group specifically",
			body: "User-agent: *\nCrawl-delay: 1\nUser-agent: Scout\nCrawl-delay: 5\n",
			want: floatPtr(5),
		},
		{
			name: "fractional crawl-delay",
			body: "User-agent: *\nCrawl-delay: 0.5\n",
			want: floatPtr(0.5),
		},
		{
			name: "a negative crawl-delay is rejected as nonsensical",
			body: "User-agent: *\nCrawl-delay: -5\n",
			want: nil,
		},
		{
			name: "an unparseable crawl-delay is ignored",
			body: "User-agent: *\nCrawl-delay: soon\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse([]byte(tt.body)).CrawlDelay()
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("CrawlDelay() = %v, want %v", got, tt.want)
			}
			if got != nil && *got != *tt.want {
				t.Fatalf("CrawlDelay() = %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestSitemaps(t *testing.T) {
	body := "Sitemap: https://example.com/sitemap1.xml\n" +
		"User-agent: *\nDisallow: /private\n" +
		"Sitemap: https://example.com/sitemap2.xml\n"

	got := Parse([]byte(body)).Sitemaps()
	want := []string{"https://example.com/sitemap1.xml", "https://example.com/sitemap2.xml"}

	if len(got) != len(want) {
		t.Fatalf("Sitemaps() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sitemaps()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllowAllAndDisallowAll(t *testing.T) {
	t.Run("AllowAll permits everything", func(t *testing.T) {
		r := AllowAll()
		for _, p := range []string{"/", "/private", "/anything/at/all"} {
			if !r.Allowed(p) {
				t.Errorf("AllowAll().Allowed(%q) = false, want true", p)
			}
		}
	})

	t.Run("DisallowAll blocks everything", func(t *testing.T) {
		r := DisallowAll()
		for _, p := range []string{"/", "/private", "/anything/at/all"} {
			if r.Allowed(p) {
				t.Errorf("DisallowAll().Allowed(%q) = true, want false", p)
			}
		}
	})
}

func TestParseNeverPanics(t *testing.T) {
	// robots.txt is attacker-influenceable: it is served by every host Scout
	// fetches from. Parse must degrade, never crash the collector.
	inputs := []string{
		"",
		":",
		"::::",
		"User-agent:",
		"User-agent: *\nDisallow",
		"\x00\x01\x02binary garbage\xff\xfe",
		"User-agent: *\n" + string(make([]byte, 10)),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", in, r)
				}
			}()
			Parse([]byte(in))
		}()
	}
}

func TestParseTruncatesOversizedBodies(t *testing.T) {
	// A body larger than MaxBodyBytes must not hang or exhaust memory on parse.
	// Build one past the limit and confirm parsing still completes and the
	// truncation point doesn't itself panic mid-multibyte-sequence handling.
	huge := make([]byte, MaxBodyBytes+1024)
	for i := range huge {
		huge[i] = 'a'
	}
	r := Parse(huge)
	// No User-agent line in that body, so the result must be unrestricted
	// rather than erroring.
	if !r.Allowed("/x") {
		t.Error("Allowed(\"/x\") = false for an oversized body with no rules, want true")
	}
}

func floatPtr(f float64) *float64 { return &f }
