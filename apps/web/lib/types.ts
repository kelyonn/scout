// Mirrors apps/api/internal/jobs/handler.go's feedItem and
// apps/api/internal/resume/handler.go's response shapes exactly — keep
// these two files in sync by hand for now (no shared codegen yet; see
// docs/04-api-design.md section 9 for where OpenAPI-generated types would
// eventually replace this).

// Mirrors apps/api/internal/jobs/state.go's validStates — the twelve
// application_state enum values (infra/migrations/000002_enums.up.sql).
export type ApplicationState =
  | "new"
  | "viewed"
  | "saved"
  | "dismissed"
  | "applied"
  | "screening"
  | "interviewing"
  | "offer"
  | "accepted"
  | "rejected"
  | "withdrawn"
  | "expired";

export interface JobFeedItem {
  job_group_id: string;
  title: string;
  company: { id: string; name: string };
  location: {
    city?: string;
    country?: string;
    tier?: number;
    work_mode: string;
  };
  compensation?: {
    normalized_inr_month: string;
    paid: string;
  };
  role_family: string;
  seniority: string;
  skills: string[];
  posted_at?: string;
  deadline_at?: string;
  first_seen_at?: string;
  priority: number;
  apply_url: string;
  state: ApplicationState;
}

export interface JobFeedResponse {
  data: JobFeedItem[];
  page: {
    next_cursor?: string;
    has_more: boolean;
  };
}

// Mirrors apps/api/internal/jobs/detail.go's detailResponse/scoresDetail.
export interface JobDetail {
  job_group_id: string;
  title: string;
  description_text?: string;
  description_html?: string;
  ai_summary?: string;
  summary_pending: boolean;
  requirements_text?: string;
  apply_url: string;
  canonical_url: string;
  role_family: string;
  seniority: string;
  location: {
    city?: string;
    country?: string;
    tier?: number;
    work_mode: string;
  };
  visa_sponsorship?: boolean;
  compensation?: {
    min?: string;
    max?: string;
    currency?: string;
    normalized_inr_month?: string;
    paid: string;
  };
  skills: string[];
  tech_stack: string[];
  posted_at?: string;
  deadline_at?: string;
  company: {
    id: string;
    name: string;
    description?: string;
    website_url?: string;
    size_bucket?: string;
    stage?: string;
    industries: string[];
  };
  scores?: {
    priority: number;
    overall_match?: number;
    skill_match?: number;
    resume_match?: number;
    company_quality?: number;
    compensation?: number;
    learning_opportunity?: number;
    engineering_culture?: number;
    growth_potential?: number;
    interview_probability?: number;
    competition_estimate?: number;
    ease_of_applying?: number;
    deadline_urgency?: number;
    location_multiplier: number;
    freshness_multiplier: number;
    explanation?: string;
  };
  source_count: number;
  first_seen_at: string;
  state: ApplicationState;
  found_elsewhere_first: boolean;
  notes?: string;
  rating?: number;
}

// Mirrors apps/api/internal/jobs/state.go's stateResponse.
export interface StateResponse {
  job_group_id: string;
  state: ApplicationState;
  state_changed_at: string;
  found_elsewhere_first: boolean;
  notes?: string;
  rating?: number;
}

// Mirrors apps/api/internal/jobs/applications.go's applicationItem.
export interface ApplicationItem {
  job_group_id: string;
  title: string;
  company: { id: string; name: string };
  location: {
    city?: string;
    country?: string;
    tier?: number;
    work_mode: string;
  };
  compensation?: {
    normalized_inr_month: string;
    paid: string;
  };
  role_family: string;
  seniority: string;
  skills: string[];
  posted_at?: string;
  deadline_at?: string;
  priority: number;
  apply_url: string;
  job_status: string;
  state: ApplicationState;
  state_changed_at: string;
  found_elsewhere_first: boolean;
  notes?: string;
  rating?: number;
  applied_at?: string;
}

export interface ApplicationsResponse {
  data: ApplicationItem[];
}

// Mirrors apps/api/internal/search/handler.go's searchResultItem.
export interface SearchResultItem {
  job_group_id: string;
  title: string;
  company: { id: string; name: string };
  location: {
    city?: string;
    country?: string;
    tier?: number;
    work_mode: string;
  };
  compensation?: {
    normalized_inr_month: string;
    paid: string;
  };
  role_family: string;
  seniority: string;
  skills: string[];
  posted_at?: string;
  priority: number;
  apply_url: string;
  state: ApplicationState;
}

export interface SearchResponse {
  data: SearchResultItem[];
  mode_served: string;
}

// Mirrors apps/api/internal/companies/handler.go's companyListItem.
export interface CompanyListItem {
  id: string;
  name: string;
  logo_url?: string;
  company_type: string;
  size_bucket?: string;
  stage?: string;
  industries: string[];
  quality_score?: number;
  hq_city?: string;
  hq_country?: string;
  open_job_count: number;
}

export interface CompaniesResponse {
  data: CompanyListItem[];
}

// Mirrors apps/api/internal/companies/handler.go's watchlistItem.
export interface WatchlistItem {
  company_id: string;
  company_name: string;
  logo_url?: string;
  company_type: string;
  notify_any_role: boolean;
  added_at: string;
  open_job_count: number;
}

export interface WatchlistResponse {
  data: WatchlistItem[];
}

// Mirrors apps/api/internal/companies/handler.go's companyDetail.
export interface CompanyDetail {
  id: string;
  name: string;
  legal_name?: string;
  description?: string;
  logo_url?: string;
  website_url?: string;
  linkedin_url?: string;
  github_org?: string;
  company_type: string;
  size_bucket?: string;
  founded_year?: number;
  stage?: string;
  industries: string[];
  tech_stack: string[];
  hq_city?: string;
  hq_country?: string;
  quality_score?: number;
  engineering_score?: number;
  open_job_count: number;
}

// Mirrors apps/api/internal/googlecalendar/routes.go's Status/Events responses.
export interface GoogleCalendarStatus {
  enabled: boolean;
  connected: boolean;
  calendar_id?: string;
  connected_at?: string;
}

export interface GoogleCalendarSyncResult {
  pushed: number;
  errors?: string[];
}

export interface GoogleCalendarEvent {
  summary: string;
  date: string;
}

export interface GoogleCalendarEventsResponse {
  data: GoogleCalendarEvent[];
}

export interface ResumeStatus {
  has_resume: boolean;
  has_embedding: boolean;
  skills: string[];
  raw_text_length: number;
  updated_at?: string;
}

export interface ResumeUploadResponse {
  ok: boolean;
  skills: string[];
  skills_added: string[] | null;
  skills_removed: string[] | null;
  raw_text_length: number;
  embedding_queued: boolean;
  resume_updated_at: string;
}

// Mirrors apps/api/internal/push/handler.go's registerDeviceRequest and
// its {"channel_id": "..."} response — ADR-012's device registration,
// called only from inside apps/mobile's own WebView (lib/capacitor.ts).
export interface RegisterDeviceRequest {
  platform: "android" | "ios";
  device_token: string;
  device_label?: string;
  app_version?: string;
}

export interface RegisterDeviceResponse {
  channel_id: string;
}
