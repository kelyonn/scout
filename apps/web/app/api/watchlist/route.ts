import { NextRequest, NextResponse } from "next/server";

// Proxies GET/POST /v1/me/watchlist — same reasoning as
// app/api/companies/route.ts (SCOUT_AUTH_TOKEN stays server-side).
export async function GET() {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const res = await fetch(new URL("/v1/me/watchlist", apiURL), {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  const body = await res.text();
  return new NextResponse(body, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}

export async function POST(request: NextRequest) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const body = await request.text();

  const res = await fetch(new URL("/v1/me/watchlist", apiURL), {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body,
    cache: "no-store",
  });

  const responseBody = await res.text();
  // The Fetch spec forbids a body on a 204 — the Go API's AddToWatchlist
  // returns exactly that (an empty string here), and NextResponse throws
  // constructing a Response with status 204 and a non-null body, even an
  // empty one.
  return new NextResponse(responseBody || null, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
