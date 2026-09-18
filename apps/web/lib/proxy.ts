// Server-only. The one place every app/api/* route handler forwards to
// the Go API through — SCOUT_AUTH_TOKEN stays server-side (it is never
// sent to browser JS, and this app has no NEXT_PUBLIC_ vars by design;
// see docs/adr/ADR-015-single-user-auth.md), and the failure modes are
// handled once rather than fifteen slightly different ways.
//
// Two behaviors here are load-bearing and were missing when each route
// held its own copy of this logic:
//
//  1. An unreachable upstream is a 502 with a JSON body, not an uncaught
//     exception. A thrown fetch() inside a route handler makes Next.js
//     answer with its own HTML 500 page, which the client then rendered
//     verbatim as "500: <!DOCTYPE html>…".
//  2. The browser's request signal is forwarded, so a client that closes
//     an SSE tab (or navigates away mid-request) tears down the upstream
//     connection too. Without it every closed live-feed tab left a
//     subscriber attached to the Go broker until the process restarted.

export const UPSTREAM_UNREACHABLE = "Scout's API isn't reachable right now. Check that the api service is running.";
export const NOT_CONFIGURED = "The web app isn't configured: SCOUT_API_URL and SCOUT_AUTH_TOKEN must be set.";

export interface ProxyOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  /** Query string to forward, including the leading "?" (or empty). */
  search?: string;
  body?: BodyInit | null;
  contentType?: string;
  headers?: Record<string, string>;
  /** Pass the upstream body through as a stream (SSE) instead of
   * buffering it. */
  stream?: boolean;
}

export function apiConfig(): { apiURL: string; token: string } | null {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) return null;
  return { apiURL, token };
}

export function jsonError(message: string, status: number): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/**
 * Forward `request` to the Go API at `path`, returning the upstream
 * response with the same status. Never throws: configuration and
 * connectivity failures become JSON error responses.
 */
export async function proxyToApi(request: Request, path: string, opts: ProxyOptions = {}): Promise<Response> {
  const config = apiConfig();
  if (!config) return jsonError(NOT_CONFIGURED, 500);

  const upstream = new URL(path, config.apiURL);
  if (opts.search) upstream.search = opts.search;

  const headers: Record<string, string> = {
    Authorization: `Bearer ${config.token}`,
    ...opts.headers,
  };
  if (opts.contentType) headers["Content-Type"] = opts.contentType;

  let res: Response;
  try {
    res = await fetch(upstream, {
      method: opts.method ?? "GET",
      headers,
      body: opts.body ?? undefined,
      cache: "no-store",
      signal: request.signal,
    });
  } catch (err) {
    // A client that went away mid-request is not an error worth logging;
    // anything else is (path only — never the token, never a body).
    if (request.signal.aborted) return new Response(null, { status: 499 });
    console.error("api proxy: upstream unreachable", { path, error: err instanceof Error ? err.name : "unknown" });
    return jsonError(UPSTREAM_UNREACHABLE, 502);
  }

  if (opts.stream) {
    return new Response(res.body, {
      status: res.status,
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      },
    });
  }

  const body = await res.text();
  // The Fetch spec forbids a body on a 204 — the Go API returns exactly
  // that for watchlist add/remove, and constructing a Response with a
  // 204 and a non-null body (even "") throws.
  return new Response(res.status === 204 || body === "" ? null : body, {
    status: res.status,
    headers: { "Content-Type": res.headers.get("Content-Type") ?? "application/json" },
  });
}
