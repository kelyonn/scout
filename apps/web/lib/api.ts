import type {
  ApplicationState,
  ApplicationsResponse,
  CompaniesResponse,
  CompanyDetail,
  GoogleCalendarEventsResponse,
  GoogleCalendarStatus,
  GoogleCalendarSyncResult,
  JobDetail,
  JobFeedResponse,
  RegisterDeviceRequest,
  RegisterDeviceResponse,
  ResumeStatus,
  ResumeUploadResponse,
  SearchResponse,
  StateResponse,
  WatchlistResponse,
} from "./types";

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
  return res.json() as Promise<T>;
}

export function fetchJobs(params: {
  minPriority?: number;
  seniority?: string[];
  roleFamily?: string[];
  workMode?: string[];
  locationTier?: string[];
  companyId?: string;
  cursor?: string;
  limit?: number;
}): Promise<JobFeedResponse> {
  const qs = new URLSearchParams();
  if (params.minPriority !== undefined) qs.set("min_priority", String(params.minPriority));
  if (params.limit !== undefined) qs.set("limit", String(params.limit));
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.companyId) qs.set("company_id", params.companyId);
  for (const s of params.seniority ?? []) qs.append("seniority", s);
  for (const r of params.roleFamily ?? []) qs.append("role_family", r);
  for (const w of params.workMode ?? []) qs.append("work_mode", w);
  for (const t of params.locationTier ?? []) qs.append("location_tier", t);
  return getJSON<JobFeedResponse>(`/api/jobs?${qs.toString()}`);
}

export function fetchCompanies(query?: string): Promise<CompaniesResponse> {
  const qs = new URLSearchParams();
  if (query) qs.set("q", query);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return getJSON<CompaniesResponse>(`/api/companies${suffix}`);
}

export function fetchCompanyDetail(id: string): Promise<CompanyDetail> {
  return getJSON<CompanyDetail>(`/api/companies/${id}`);
}

export function fetchWatchlist(): Promise<WatchlistResponse> {
  return getJSON<WatchlistResponse>("/api/watchlist");
}

export async function addToWatchlist(companyId: string, notifyAnyRole = true): Promise<void> {
  const res = await fetch("/api/watchlist", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ company_id: companyId, notify_any_role: notifyAnyRole }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
}

export async function removeFromWatchlist(companyId: string): Promise<void> {
  const res = await fetch(`/api/watchlist/${companyId}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
}

export function fetchGoogleCalendarStatus(): Promise<GoogleCalendarStatus> {
  return getJSON<GoogleCalendarStatus>("/api/integrations/google/status");
}

export async function syncGoogleCalendar(): Promise<GoogleCalendarSyncResult> {
  const res = await fetch("/api/integrations/google/sync", { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
  return res.json() as Promise<GoogleCalendarSyncResult>;
}

export async function disconnectGoogleCalendar(): Promise<void> {
  const res = await fetch("/api/integrations/google/disconnect", { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
}

export function fetchGoogleCalendarEvents(): Promise<GoogleCalendarEventsResponse> {
  return getJSON<GoogleCalendarEventsResponse>("/api/integrations/google/events");
}

export function fetchJobDetail(groupId: string): Promise<JobDetail> {
  return getJSON<JobDetail>(`/api/jobs/${groupId}`);
}

export async function updateJobState(
  groupId: string,
  params: { state: ApplicationState; foundElsewhereFirst?: boolean; notes?: string; rating?: number },
): Promise<StateResponse> {
  const res = await fetch(`/api/jobs/${groupId}/state`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      state: params.state,
      found_elsewhere_first: params.foundElsewhereFirst,
      notes: params.notes,
      rating: params.rating,
    }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
  return res.json() as Promise<StateResponse>;
}

export function fetchApplications(states?: ApplicationState[]): Promise<ApplicationsResponse> {
  const qs = new URLSearchParams();
  for (const s of states ?? []) qs.append("state", s);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return getJSON<ApplicationsResponse>(`/api/applications${suffix}`);
}

export function searchJobs(
  query: string,
  filters?: { seniority?: string[]; roleFamily?: string[]; workMode?: string[] },
): Promise<SearchResponse> {
  const qs = new URLSearchParams({ q: query });
  for (const s of filters?.seniority ?? []) qs.append("seniority", s);
  for (const r of filters?.roleFamily ?? []) qs.append("role_family", r);
  for (const w of filters?.workMode ?? []) qs.append("work_mode", w);
  return getJSON<SearchResponse>(`/api/search?${qs.toString()}`);
}

export function fetchResumeStatus(): Promise<ResumeStatus> {
  return getJSON<ResumeStatus>("/api/resume");
}

export async function uploadResume(file: File): Promise<ResumeUploadResponse> {
  const form = new FormData();
  form.set("resume", file);
  const res = await fetch("/api/resume", { method: "POST", body: form });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
  return res.json() as Promise<ResumeUploadResponse>;
}

export async function registerDevice(req: RegisterDeviceRequest): Promise<RegisterDeviceResponse> {
  const res = await fetch("/api/push/register-device", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status}: ${body}`);
  }
  return res.json() as Promise<RegisterDeviceResponse>;
}
