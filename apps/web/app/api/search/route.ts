import { NextRequest, NextResponse } from "next/server";

// Thin server-side proxy to GET /v1/search — same reasoning as
// app/api/jobs/route.ts (token stays server-side only).
export async function GET(request: NextRequest) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const upstream = new URL("/v1/search", apiURL);
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
