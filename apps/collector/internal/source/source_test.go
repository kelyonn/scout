package source

import "testing"

func TestRegisteredDomain(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "shared ATS subdomain collapses to the registered domain",
			// The case the whole method exists for: boards.greenhouse.io serves
			// thousands of sources and must rate-limit as one.
			url:  "https://boards.greenhouse.io/cloudflare",
			want: "greenhouse.io",
		},
		{
			name: "a bare registered domain returns itself",
			url:  "https://lever.co/some/path",
			want: "lever.co",
		},
		{
			name: "a multi-level suffix is handled correctly",
			// co.in is a public suffix, so the registered domain is one level
			// deeper than a naive "last two labels" split would produce.
			url:  "https://careers.example.co.in/jobs",
			want: "example.co.in",
		},
		{
			name: "different subdomains of the same company collapse together",
			url:  "https://jobs.example.com/board",
			want: "example.com",
		},
		{
			name:    "empty url is an error",
			url:     "",
			wantErr: true,
		},
		{
			name:    "a url with no host is an error",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name: "an unknown-suffix hostname falls back to itself rather than erroring",
			// The gate must stay usable for test fixtures and internal tools
			// that use a bare hostname with no recognised public suffix.
			url:  "http://localhost:8080/board",
			want: "localhost",
		},
		{
			name: "an IPv4 literal returns itself rather than a public-suffix guess",
			// Regression test: publicsuffix.EffectiveTLDPlusOne does not know
			// "127.0.0.1" is an IP rather than a domain name, splits it on '.'
			// as if it were one, and returns a nonsense answer like "0.1". Every
			// httptest.Server in this codebase binds to 127.0.0.1, so this bug
			// silently folded every local-server-backed politeness-gate test
			// into one shared, wrong rate-limit bucket before it was caught.
			url:  "http://127.0.0.1:54321/board",
			want: "127.0.0.1",
		},
		{
			name: "an IPv6 literal returns itself",
			url:  "http://[::1]:8080/board",
			want: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Source{URL: tt.url}
			got, err := s.RegisteredDomain()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("RegisteredDomain(%q) = %q, want an error", tt.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("RegisteredDomain(%q): unexpected error: %v", tt.url, err)
			}
			if got != tt.want {
				t.Errorf("RegisteredDomain(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}

	t.Run("two sources on the same shared host produce the same domain", func(t *testing.T) {
		// This is the property the rate limiter depends on: two different
		// Source rows for two different companies, both on Greenhouse, must key
		// to the same bucket.
		a := Source{URL: "https://boards.greenhouse.io/cloudflare"}
		b := Source{URL: "https://boards.greenhouse.io/anthropic"}

		domainA, err := a.RegisteredDomain()
		if err != nil {
			t.Fatalf("a.RegisteredDomain: %v", err)
		}
		domainB, err := b.RegisteredDomain()
		if err != nil {
			t.Fatalf("b.RegisteredDomain: %v", err)
		}
		if domainA != domainB {
			t.Errorf("domains differ: %q vs %q, want the same bucket", domainA, domainB)
		}
	})
}
