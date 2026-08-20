import { NextResponse } from "next/server";

// Streaming proxy to GET /v1/stream (SSE) — same token-stays-server-side
// reasoning as every other app/api/* route, but unlike the others this
// one must not buffer: the upstream response body is piped straight
// through as it arrives, which is what makes this a live stream rather
// than a very slow JSON response. Browser EventSource can't send a custom
// Authorization header itself, which is exactly why this proxy exists.
export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
  const lastEventId = request.headers.get("Last-Event-ID");
  if (lastEventId) headers["Last-Event-ID"] = lastEventId;

  const upstream = await fetch(new URL("/v1/stream", apiURL), {
    headers,
    cache: "no-store",
  });

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    },
  });
}
