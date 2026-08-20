package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

func TestURLFor(t *testing.T) {
	cases := []struct {
		kind padapter.SourceKind
		want string
	}{
		{padapter.SourceKindGreenhouse, "https://boards-api.greenhouse.io/v1/boards/acme/jobs?content=true"},
		{padapter.SourceKindLever, "https://api.lever.co/v0/postings/acme?mode=json"},
		{padapter.SourceKindAshby, "https://api.ashbyhq.com/posting-api/job-board/acme?includeCompensation=true"},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			got, err := URLFor("acme", c.kind)
			if err != nil {
				t.Fatalf("URLFor: %v", err)
			}
			if got != c.want {
				t.Errorf("URLFor(%q) = %q, want %q", c.kind, got, c.want)
			}
		})
	}

	if _, err := URLFor("acme", "ats_bamboohr"); err == nil {
		t.Error("expected an error for an unsupported source kind")
	}
}

// fakeAdapter is a scripted padapter.Adapter, mirroring
// apps/collector/internal/scheduler_test.go's fakeGate/fakeFetcher pattern —
// Assess must never touch the network in a unit test, and this is the
// interface seam that lets it not.
type fakeAdapter struct {
	kind       padapter.SourceKind
	statusCode int
	postings   []schema.Posting
	fetchErr   error
	parseErr   error
}

func (f *fakeAdapter) Kind() padapter.SourceKind { return f.kind }

func (f *fakeAdapter) Fetch(_ context.Context, _ padapter.Source, _ padapter.FetchHints) (*padapter.RawResponse, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	code := f.statusCode
	if code == 0 {
		code = http.StatusOK
	}
	return &padapter.RawResponse{StatusCode: code}, nil
}

func (f *fakeAdapter) Parse(_ context.Context, _ padapter.Source, _ *padapter.RawResponse) ([]schema.Posting, error) {
	if f.parseErr != nil {
		return nil, f.parseErr
	}
	return f.postings, nil
}

func (f *fakeAdapter) Validate(_ context.Context, _ padapter.Source, _ []schema.Posting) error {
	return nil
}

func newAssessor(ad *fakeAdapter) *Assessor {
	return &Assessor{
		Adapters: map[padapter.SourceKind]padapter.Adapter{ad.kind: ad},
		Roles:    taxonomy.LoadRolePatterns(),
		Skills:   taxonomy.LoadSkills(),
	}
}

func TestAssess_RelevantOnSoftwareInternship(t *testing.T) {
	ad := &fakeAdapter{
		kind: padapter.SourceKindGreenhouse,
		postings: []schema.Posting{
			{TitleRaw: "VP of Sales"}, // not software — must not short-circuit relevance
			{TitleRaw: "Software Engineering Intern"},
		},
	}
	res, err := newAssessor(ad).Assess(context.Background(), Candidate{Slug: "acme", Kind: ad.kind})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !res.Relevant {
		t.Error("expected Relevant = true: one posting is a software internship")
	}
	if res.JobCount != 2 {
		t.Errorf("JobCount = %d, want 2", res.JobCount)
	}
}

func TestAssess_NotRelevantWhenOnlySeniorSoftwareRoles(t *testing.T) {
	ad := &fakeAdapter{
		kind: padapter.SourceKindGreenhouse,
		postings: []schema.Posting{
			{TitleRaw: "Staff Software Engineer"},
			{TitleRaw: "Principal Engineer"},
		},
	}
	res, err := newAssessor(ad).Assess(context.Background(), Candidate{Slug: "acme", Kind: ad.kind})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if res.Relevant {
		t.Error("expected Relevant = false: every software posting is senior/staff level")
	}
}

func TestAssess_NotRelevantWhenNoSoftwareRoles(t *testing.T) {
	ad := &fakeAdapter{
		kind: padapter.SourceKindGreenhouse,
		postings: []schema.Posting{
			{TitleRaw: "Regional Sales Director"},
			{TitleRaw: "Marketing Intern"},
		},
	}
	res, err := newAssessor(ad).Assess(context.Background(), Candidate{Slug: "acme", Kind: ad.kind})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if res.Relevant {
		t.Error("expected Relevant = false: neither posting is a software role, regardless of seniority")
	}
}

func TestAssess_EmptyBoardIsNotAnError(t *testing.T) {
	ad := &fakeAdapter{kind: padapter.SourceKindAshby, postings: nil}
	res, err := newAssessor(ad).Assess(context.Background(), Candidate{Slug: "acme", Kind: ad.kind})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if res.Relevant || res.JobCount != 0 {
		t.Errorf("empty board: got Relevant=%v JobCount=%d, want false/0", res.Relevant, res.JobCount)
	}
}

func TestAssess_FetchFailureIsNotARunError(t *testing.T) {
	ad := &fakeAdapter{kind: padapter.SourceKindLever, fetchErr: context.DeadlineExceeded}
	res, err := newAssessor(ad).Assess(context.Background(), Candidate{Slug: "acme", Kind: ad.kind})
	if err != nil {
		t.Fatalf("Assess should absorb a single candidate's fetch failure, not fail the run: %v", err)
	}
	if res.Relevant {
		t.Error("a fetch failure must not read as relevant")
	}
}

func TestAssess_UnregisteredKindErrors(t *testing.T) {
	a := &Assessor{Adapters: map[padapter.SourceKind]padapter.Adapter{}}
	_, err := a.Assess(context.Background(), Candidate{Slug: "acme", Kind: padapter.SourceKindGreenhouse})
	if err == nil {
		t.Error("expected an error when no adapter is registered for the candidate's kind")
	}
}

func TestFetchSlugList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"acme", "beta-corp", "gamma"})
	}))
	defer srv.Close()

	slugs, err := FetchSlugList(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchSlugList: %v", err)
	}
	want := []string{"acme", "beta-corp", "gamma"}
	if len(slugs) != len(want) {
		t.Fatalf("got %d slugs, want %d", len(slugs), len(want))
	}
	for i, s := range want {
		if slugs[i] != s {
			t.Errorf("slugs[%d] = %q, want %q", i, slugs[i], s)
		}
	}
}

func TestFetchSlugList_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := FetchSlugList(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected an error on a non-200 response")
	}
}
