import { NextResponse } from "next/server";

// Proxies GET /v1/jobs/{group_id} — same reasoning as
// app/api/jobs/route.ts (token stays server-side only).
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ groupId: string }> },
) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const { groupId } = await params;
  const res = await fetch(new URL(`/v1/jobs/${groupId}`, apiURL), {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  const body = await res.text();
  return new NextResponse(body, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
