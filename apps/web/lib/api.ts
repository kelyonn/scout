import type {
  ApplicationState,
  ApplicationsResponse,
  JobDetail,
  JobFeedResponse,
  ResumeStatus,
  ResumeUploadResponse,
  SearchResponse,
  StateResponse,
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
  cursor?: string;
  limit?: number;
}): Promise<JobFeedResponse> {
  const qs = new URLSearchParams();
  if (params.minPriority !== undefined) qs.set("min_priority", String(params.minPriority));
  if (params.limit !== undefined) qs.set("limit", String(params.limit));
  if (params.cursor) qs.set("cursor", params.cursor);
  for (const s of params.seniority ?? []) qs.append("seniority", s);
  for (const r of params.roleFamily ?? []) qs.append("role_family", r);
  return getJSON<JobFeedResponse>(`/api/jobs?${qs.toString()}`);
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

export function searchJobs(query: string): Promise<SearchResponse> {
  const qs = new URLSearchParams({ q: query });
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
