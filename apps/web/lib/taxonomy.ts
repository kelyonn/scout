// Display labels and filter option lists for the enums job/job_group rows
// carry (role_family, seniority, work_mode, location_tier) — packages/db's
// generated types keep these as plain strings, so the mapping lives here
// once rather than as ad-hoc literals scattered across JobCard, FilterBar,
// and every page that renders a job. Mirrors
// apps/notifier/internal/deliver's formatRoleFamily/formatSeniority/
// formatWorkMode (Go, for Telegram messages) — same enum values, same
// display conventions, kept in sync by hand since the two live in
// different languages with no shared codegen for this.

export const SENIORITY_LABEL: Record<string, string> = {
  internship: "Internship",
  apprenticeship: "Apprenticeship",
  new_grad: "New grad",
  entry: "Entry",
  mid: "Mid",
  senior: "Senior",
  staff: "Staff",
  unknown: "Unspecified",
};

export const SENIORITY_OPTIONS = [
  "internship",
  "new_grad",
  "entry",
  "mid",
  "senior",
  "staff",
  "unknown",
];

export const WORK_MODE_LABEL: Record<string, string> = {
  onsite: "On-site",
  hybrid: "Hybrid",
  remote: "Remote",
  unknown: "Unspecified",
};

export const WORK_MODE_OPTIONS = ["onsite", "hybrid", "remote"];

export const LOCATION_TIER_LABEL: Record<string, string> = {
  "1": "Tier 1 — Bengaluru",
  "2": "Tier 2 — Rest of India",
  "3": "Tier 3 — Remote (India-eligible)",
  "4": "Tier 4 — International",
};

export const LOCATION_TIER_OPTIONS = ["1", "2", "3", "4"];

// docs/07-normalization-taxonomy.md's full role_family enum — order here
// is the display order in FilterBar, grouped visually by prefix.
export const ROLE_FAMILY_OPTIONS = [
  "swe.general",
  "swe.backend",
  "swe.frontend",
  "swe.fullstack",
  "swe.mobile",
  "swe.data",
  "swe.ml",
  "swe.ml.research",
  "swe.infra",
  "swe.infra.sre",
  "swe.infra.devops",
  "swe.infra.platform",
  "swe.infra.cloud",
  "swe.systems",
  "swe.security",
  "swe.embedded",
  "swe.qa",
  "swe.research",
  "swe.other",
  "advocacy.devrel",
  "advocacy.devex",
  "advocacy.solutions",
  // Beyond software (ADR-020).
  "product.management",
  "product.design",
  "product.analyst",
  "consulting.strategy",
  "consulting.technology",
  "business.strategy",
  "business.operations",
  "business.general",
];

// formatRoleFamily turns "swe.infra.devops" into "Software Engineering
// (Infra/Devops)" — the family prefix names the broad family (only "swe"
// and "advocacy" exist today), and ".general"/".other" carry no more
// information than the family itself, so they're dropped rather than
// shown as a redundant suffix. Same logic as
// apps/notifier/internal/deliver.formatRoleFamily (Go).
export function formatRoleFamily(roleFamily: string): string {
  const segments = roleFamily.split(".");
  if (segments.length === 0 || !segments[0]) return "";
  const familyKey = segments[0];
  const family = familyKey === "swe" ? "Software Engineering" : familyKey === "advocacy" ? "Advocacy" : capitalize(familyKey);
  const specifics = segments
    .slice(1)
    .filter((s) => s && s !== "general" && s !== "other")
    .map(capitalize);
  return specifics.length === 0 ? family : `${family} (${specifics.join("/")})`;
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
