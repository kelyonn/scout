import { proxyToApi } from "@/lib/proxy";

// Streaming proxy to GET /v1/stream (SSE). The browser's EventSource
// can't send an Authorization header, which is why this proxy exists;
// lib/proxy.ts forwards the request's abort signal so closing the tab
// closes the upstream subscription too.
export const dynamic = "force-dynamic";

export function GET(request: Request) {
  const lastEventId = request.headers.get("Last-Event-ID");
  return proxyToApi(request, "/v1/stream", {
    stream: true,
    headers: lastEventId ? { "Last-Event-ID": lastEventId } : undefined,
  });
}
