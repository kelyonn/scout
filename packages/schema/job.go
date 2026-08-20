package schema

import "time"

// RoleFamily mirrors the role_family enum (infra/migrations/000002_enums.up.sql).
// A typed string rather than the sqlc-generated db.RoleFamily so this package
// has no dependency on packages/db/gen — normalize/classify produce values of
// this type, and the caller converts at the insert boundary.
type RoleFamily string

// The 21 role_family enum values (infra/migrations/000002_enums.up.sql),
// the docs/07 section 4 taxonomy's leaves plus its swe.other catch-all.
const (
	RoleSWEGeneral        RoleFamily = "swe.general"
	RoleSWEBackend        RoleFamily = "swe.backend"
	RoleSWEFrontend       RoleFamily = "swe.frontend"
	RoleSWEFullstack      RoleFamily = "swe.fullstack"
	RoleSWEMobile         RoleFamily = "swe.mobile"
	RoleSWEML             RoleFamily = "swe.ml"
	RoleSWEMLResearch     RoleFamily = "swe.ml.research"
	RoleSWEData           RoleFamily = "swe.data"
	RoleSWEInfra          RoleFamily = "swe.infra"
	RoleSWEInfraSRE       RoleFamily = "swe.infra.sre"
	RoleSWEInfraDevOps    RoleFamily = "swe.infra.devops"
	RoleSWEInfraPlatform  RoleFamily = "swe.infra.platform"
	RoleSWEInfraCloud     RoleFamily = "swe.infra.cloud"
	RoleSWESystems        RoleFamily = "swe.systems"
	RoleSWESecurity       RoleFamily = "swe.security"
	RoleSWEEmbedded       RoleFamily = "swe.embedded"
	RoleSWEQA             RoleFamily = "swe.qa"
	RoleSWEResearch       RoleFamily = "swe.research"
	RoleAdvocacyDevRel    RoleFamily = "advocacy.devrel"
	RoleAdvocacyDevEx     RoleFamily = "advocacy.devex"
	RoleAdvocacySolutions RoleFamily = "advocacy.solutions"
	RoleSWEOther          RoleFamily = "swe.other"
)

// Seniority mirrors the seniority enum.
type Seniority string

// The seniority enum values, docs/07 section 5.
const (
	SeniorityInternship     Seniority = "internship"
	SeniorityApprenticeship Seniority = "apprenticeship"
	SeniorityNewGrad        Seniority = "new_grad"
	SeniorityEntry          Seniority = "entry"
	SeniorityMid            Seniority = "mid"
	SenioritySenior         Seniority = "senior"
	SeniorityStaff          Seniority = "staff"
	SeniorityUnknown        Seniority = "unknown"
)

// PaidSignal mirrors the paid_signal enum. 'unknown' is not 'unpaid' — see
// docs/07 section 7.
type PaidSignal string

// The paid_signal enum values.
const (
	PaidYes     PaidSignal = "paid"
	PaidNo      PaidSignal = "unpaid"
	PaidUnknown PaidSignal = "unknown"
)

// WorkMode mirrors the work_mode enum.
type WorkMode string

// The work_mode enum values.
const (
	WorkOnsite  WorkMode = "onsite"
	WorkHybrid  WorkMode = "hybrid"
	WorkRemote  WorkMode = "remote"
	WorkUnknown WorkMode = "unknown"
)

// NormalizedJob is the output of the normalize -> classify pipeline for one
// posting: docs/07's canonical shape, restricted to the fields P1 actually
// computes. It is not yet attached to a job_group or scored — dedup and
// scoring are the next two pipeline stages, each documented in their own
// package.
type NormalizedJob struct {
	// Identity (docs/07 section 3)
	CanonicalURL     string
	CanonicalURLHash []byte // sha256(CanonicalURL)
	ATSPlatform      string
	ATSJobID         string
	ContentHash      []byte

	// Content
	Title            string
	NormalizedTitle  string
	DescriptionHTML  string
	DescriptionText  string
	RequirementsText string
	ApplyURL         string

	// Classification (docs/07 sections 4-5, Tier 0 only)
	RoleFamily     RoleFamily
	RoleConfidence float32
	Seniority      Seniority
	IsSoftware     bool
	Skills         []string // ontology IDs, docs/07 section 8

	// Location (docs/07 section 6)
	LocationRaw     string
	LocationCity    string
	LocationRegion  string
	LocationCountry string // ISO 3166-1 alpha-2
	LocationTier    int16  // 1-4, 0 = unresolved
	WorkMode        WorkMode
	VisaSponsorship *bool

	// Compensation (docs/07 section 7)
	Paid                   PaidSignal
	CompMin                *float64
	CompMax                *float64
	CompCurrency           string
	CompPeriod             string // hour|month|year|stipend_total
	CompNormalizedINRMonth *float64
	CompConfidence         *float32
	PrestigeException      bool

	// Lifecycle
	PostedAt          *time.Time
	PostedAtEstimated bool
	DeadlineAt        *time.Time
}
