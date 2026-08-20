import { NextRequest, NextResponse } from "next/server";

// Thin server-side proxy to the real Go API (GET/POST /v1/resume) — same
// reasoning as app/api/jobs/route.ts.
function apiConfig() {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) return null;
  return { apiURL, token };
}

export async function GET() {
  const config = apiConfig();
  if (!config) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const res = await fetch(new URL("/v1/resume", config.apiURL), {
    headers: { Authorization: `Bearer ${config.token}` },
    cache: "no-store",
  });

  const body = await res.text();
  return new NextResponse(body, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}

export async function POST(request: NextRequest) {
  const config = apiConfig();
  if (!config) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  // Pass the multipart body straight through — this route never parses
  // the PDF itself, the Go handler does.
  const contentType = request.headers.get("content-type") ?? "";
  const body = await request.arrayBuffer();

  const res = await fetch(new URL("/v1/resume", config.apiURL), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${config.token}`,
      "Content-Type": contentType,
    },
    body,
  });

  const text = await res.text();
  return new NextResponse(text, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
