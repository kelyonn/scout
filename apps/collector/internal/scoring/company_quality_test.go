package scoring

import (
	"testing"

	"github.com/kelyon/scout/packages/schema"
)

func TestComputeCompanyQuality_AllSignalsPresentScoresHigh(t *testing.T) {
	score, inputs := computeCompanyQuality(
		schema.RoleSWEInfra,
		"We run everything on Kubernetes with Go services and gRPC. Our structured intern program pairs every intern with a dedicated mentor.",
	)
	if score < 90 {
		t.Errorf("score = %d, want close to 100 with every signal present", score)
	}
	if inputs["tech_stack_modernity_signal"] != true {
		t.Error("expected tech_stack_modernity_signal = true")
	}
	if inputs["intern_program_signal"] != true {
		t.Error("expected intern_program_signal = true")
	}
	if inputs["in_domain_interest"] != true {
		t.Error("expected in_domain_interest = true for swe.infra")
	}
}

func TestComputeCompanyQuality_NoSignalsScoresLow(t *testing.T) {
	score, inputs := computeCompanyQuality(schema.RoleSWEFrontend, "Join our team and build great products.")
	if score > 60 {
		t.Errorf("score = %d, want low with no signals present", score)
	}
	if inputs["in_domain_interest"] != false {
		t.Error("expected in_domain_interest = false for swe.frontend")
	}
}

func TestComputeCompanyQuality_MarksPartial(t *testing.T) {
	_, inputs := computeCompanyQuality(schema.RoleSWEGeneral, "")
	if inputs["partial"] != true {
		t.Error("expected partial = true, this is a 3-of-7-term computation")
	}
}
