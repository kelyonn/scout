import { NextResponse } from "next/server";

// Proxies POST /v1/jobs/{group_id}/state — same reasoning as the sibling
// routes under app/api/jobs (token stays server-side only).
export async function POST(
  request: Request,
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
  const body = await request.text();

  const res = await fetch(new URL(`/v1/jobs/${groupId}/state`, apiURL), {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body,
    cache: "no-store",
  });

  const responseBody = await res.text();
  return new NextResponse(responseBody, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
