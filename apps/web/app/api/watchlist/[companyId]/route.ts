import { NextResponse } from "next/server";

// Proxies DELETE /v1/me/watchlist/{company_id} — same reasoning as the
// sibling routes under app/api/watchlist.
export async function DELETE(
  _request: Request,
  { params }: { params: Promise<{ companyId: string }> },
) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const { companyId } = await params;

  const res = await fetch(new URL(`/v1/me/watchlist/${companyId}`, apiURL), {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  const body = await res.text();
  // See app/api/watchlist/route.ts's identical comment — RemoveFromWatchlist
  // returns 204 too.
  return new NextResponse(body || null, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
