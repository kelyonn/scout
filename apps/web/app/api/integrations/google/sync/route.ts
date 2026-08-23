import { NextResponse } from "next/server";

// Proxies POST /v1/integrations/google/sync.
export async function POST() {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const res = await fetch(new URL("/v1/integrations/google/sync", apiURL), {
    method: "POST",
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  const body = await res.text();
  return new NextResponse(body, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
