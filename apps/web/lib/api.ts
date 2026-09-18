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

// Every call the browser makes goes through here. The Go API's error
// bodies are `{"error": "…"}` written for a developer reading logs
// ("invalid transition: saved -> applied"); ApiError carries that as
// `detail` and exposes a `message` a person can act on.
export class ApiError extends Error {
  readonly status: number;
  /** The server's own error string, when it sent one. */
  readonly detail: string | null;

  constructor(status: number, message: string, detail: string | null = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }
}

export const NETWORK_ERROR_MESSAGE = "Couldn't reach Scout. Check your connection and try again.";

/** Turns anything a query or mutation rejected with into one sentence
 * fit for the UI. */
export function errorMessage(err: unknown, fallback = "Something went wrong. Try again."): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}

function messageFor(status: number, detail: string | null): string {
  switch (status) {
    case 400:
      return detail ? `The request wasn't valid: ${detail}.` : "The request wasn't valid.";
    case 401:
      return "Your session has ended. Sign in again to continue.";
    case 403:
      return "You don't have access to that.";
    case 404:
      return "That no longer exists — it may have been removed or closed.";
    case 409:
      return "That change isn't possible from the current state. Refresh to see the latest.";
    case 412:
      return detail ? `${capitalize(detail)}.` : "Something needs to be set up first.";
    case 413:
      return "That file is too large.";
    case 422:
      return detail ? `${capitalize(detail)}.` : "The server couldn't process that input.";
    case 429:
      return "Too many attempts. Wait a moment and try again.";
    case 502:
    case 503:
    case 504:
      return detail ?? "Scout's API isn't reachable right now.";
    default:
      return "Something went wrong on the server. Try again in a moment.";
  }
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

async function readDetail(res: Response): Promise<string | null> {
  const text = await res.text().catch(() => "");
  if (!text) return null;
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === "object" && "error" in parsed && typeof parsed.error === "string") {
      return parsed.error;
    }
  } catch {
    // Not JSON (an HTML error page from something in between) — never
    // surface markup to the user.
  }
  return null;
}

// A 401 means the session cookie is gone or the token was rotated. Every
// screen would otherwise show the same dead-end error, so the client
// sends the browser back through /login once, preserving where it was.
let redirectingToLogin = false;
function redirectToLogin(): void {
  if (typeof window === "undefined" || redirectingToLogin) return;
  if (window.location.pathname.startsWith("/login")) return;
  redirectingToLogin = true;
  const next = window.location.pathname + window.location.search;
  // A full navigation on purpose, not router.push: the client router's
  // cache still holds the pages that just 401'd, and /login's own
  // redirect back needs a fresh server render to see the new cookie.
  // eslint-disable-next-line @next/next/no-location-assign-relative-destination -- see above
  window.location.href = `/login?next=${encodeURIComponent(next)}`;
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch {
    throw new ApiError(0, NETWORK_ERROR_MESSAGE);
  }

  if (!res.ok) {
    const detail = await readDetail(res);
    if (res.status === 401) redirectToLogin();
    throw new ApiError(res.status, messageFor(res.status, detail), detail);
  }

  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

function getJSON<T>(url: string): Promise<T> {
  return request<T>(url);
}

function postJSON<T>(url: string, body?: unknown): Promise<T> {
  return request<T>(url, {
    method: "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
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
  return getJSON<CompanyDetail>(`/api/companies/${encodeURIComponent(id)}`);
}

export function fetchWatchlist(): Promise<WatchlistResponse> {
  return getJSON<WatchlistResponse>("/api/watchlist");
}

export async function addToWatchlist(companyId: string, notifyAnyRole = true): Promise<void> {
  await postJSON<void>("/api/watchlist", { company_id: companyId, notify_any_role: notifyAnyRole });
}

export async function removeFromWatchlist(companyId: string): Promise<void> {
  await request<void>(`/api/watchlist/${encodeURIComponent(companyId)}`, { method: "DELETE" });
}

export function fetchGoogleCalendarStatus(): Promise<GoogleCalendarStatus> {
  return getJSON<GoogleCalendarStatus>("/api/integrations/google/status");
}

export function syncGoogleCalendar(): Promise<GoogleCalendarSyncResult> {
  return postJSON<GoogleCalendarSyncResult>("/api/integrations/google/sync");
}

export async function disconnectGoogleCalendar(): Promise<void> {
  await postJSON<void>("/api/integrations/google/disconnect");
}

export function fetchGoogleCalendarEvents(): Promise<GoogleCalendarEventsResponse> {
  return getJSON<GoogleCalendarEventsResponse>("/api/integrations/google/events");
}

export function fetchJobDetail(groupId: string): Promise<JobDetail> {
  return getJSON<JobDetail>(`/api/jobs/${encodeURIComponent(groupId)}`);
}

export function updateJobState(
  groupId: string,
  params: { state: ApplicationState; foundElsewhereFirst?: boolean; notes?: string; rating?: number },
): Promise<StateResponse> {
  return postJSON<StateResponse>(`/api/jobs/${encodeURIComponent(groupId)}/state`, {
    state: params.state,
    found_elsewhere_first: params.foundElsewhereFirst,
    notes: params.notes,
    rating: params.rating,
  });
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

export function uploadResume(file: File): Promise<ResumeUploadResponse> {
  const form = new FormData();
  form.set("resume", file);
  return request<ResumeUploadResponse>("/api/resume", { method: "POST", body: form });
}

export function registerDevice(req: RegisterDeviceRequest): Promise<RegisterDeviceResponse> {
  return postJSON<RegisterDeviceResponse>("/api/push/register-device", req);
}
