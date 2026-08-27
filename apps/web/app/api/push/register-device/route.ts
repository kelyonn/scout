import { NextRequest, NextResponse } from "next/server";

// Proxies POST /v1/push/register-device — same reasoning as
// app/api/watchlist/route.ts (SCOUT_AUTH_TOKEN stays server-side).
// Called by apps/mobile's own WebView (via lib/capacitor.ts's
// registerDeviceToken) after @capacitor/push-notifications hands the app
// a real FCM token — this route never sees the token's origin, it just
// forwards whatever body it's given to the Go API's own validation.
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

  const res = await fetch(new URL("/v1/push/register-device", apiURL), {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body,
    cache: "no-store",
  });

  const responseBody = await res.text();
  return new NextResponse(responseBody || null, {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
