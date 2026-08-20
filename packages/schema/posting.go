// Package schema holds the canonical types shared across the ingestion
// pipeline: an adapter's raw Parse output (Posting) and the normalized
// fields that feed the job table.
//
// The README describes a JSON-Schema-driven codegen pipeline producing Go
// structs, Pydantic models, and TypeScript types — that is real
// infrastructure, and nothing needs it yet: apps/brain (Python) and the
// schema-consuming parts of apps/web don't exist until P2/P3. This package
// is the plain-Go version for now, the same "add tooling when something
// needs it" call P0 made for Python and pnpm. Migrating to generated code
// is mechanical once a second language actually reads this shape.
package schema

import "time"

// Posting is an adapter's raw, faithful extraction from one source — see
// docs/07-normalization-taxonomy.md section 2. An adapter must not guess at
// a location tier or role family; that is the normalizer's job. Fields are
// deliberately close to what a source actually says, not what Scout wants
// it to mean.
type Posting struct {
	// Identity
	ExternalID string // ATS-native job id, when the source has one
	URL        string // as fetched
	ApplyURL   string

	// Company
	CompanyNameRaw string
	DomainHint     string
	ATSToken       string

	// Content
	TitleRaw          string
	DescriptionHTML   string
	DescriptionText   string
	RequirementsText  string
	Department        string
	EmploymentTypeRaw string

	// Location — a job may list several; the normalizer resolves and tiers them.
	LocationRaw string
	RemoteHint  bool

	// Compensation, as stated by the source. Empty/zero when absent —
	// extraction confidence is the normalizer's job, not the adapter's.
	CompensationRawText string

	// Timing
	PostedAt          *time.Time
	PostedAtEstimated bool
	DeadlineAt        *time.Time

	// Provenance
	Adapter        string
	AdapterVersion string
	FetchedAt      time.Time
	ContentHash    []byte
}
