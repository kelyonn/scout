import { NextRequest, NextResponse } from "next/server";

// Thin server-side proxy to the real Go API (GET /v1/companies) — same
// reasoning as app/api/jobs/route.ts (SCOUT_AUTH_TOKEN stays server-side).
export async function GET(request: NextRequest) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const upstream = new URL("/v1/companies", apiURL);
  upstream.search = request.nextUrl.search;

  const res = await fetch(upstream, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  const body = await res.text();
  return new NextResponse(body, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
