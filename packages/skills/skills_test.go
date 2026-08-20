package skills

import (
	"testing"

	"github.com/kelyon/scout/packages/taxonomy"
)

func TestExtract(t *testing.T) {
	ontology := taxonomy.LoadSkills()
	got := Extract("Looking for someone with Go, Docker, and Kubernetes experience. Bonus: React.", ontology)

	want := map[string]bool{"go": true, "docker": true, "kubernetes": true, "react": true}
	if len(got) != len(want) {
		t.Fatalf("Extract returned %v, want %d matches", got, len(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected skill id %q", id)
		}
	}
}
