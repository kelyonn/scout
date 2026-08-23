import { NextRequest, NextResponse } from "next/server";

const STATE_COOKIE = "scout_google_oauth_state";

// Google redirects the browser here after consent. The actual token
// exchange (and the client secret it needs) happens server-side in the
// Go API — apps/api/internal/googlecalendar.Handler.Callback — this
// route's job is just: verify the CSRF state survived the round trip,
// forward the code, and send the browser back to Settings.
export async function GET(request: NextRequest) {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.redirect(new URL("/settings?google=error", request.url));
  }

  const code = request.nextUrl.searchParams.get("code");
  const state = request.nextUrl.searchParams.get("state");
  const expectedState = request.cookies.get(STATE_COOKIE)?.value;

  if (!code || !state || !expectedState || state !== expectedState) {
    return NextResponse.redirect(new URL("/settings?google=error", request.url));
  }

  const redirectURI = new URL("/api/integrations/google/callback", request.url).toString();

  const res = await fetch(new URL("/v1/integrations/google/callback", apiURL), {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ code, redirect_uri: redirectURI }),
    cache: "no-store",
  });

  const redirect = NextResponse.redirect(
    new URL(res.ok ? "/settings?google=connected" : "/settings?google=error", request.url),
  );
  redirect.cookies.set(STATE_COOKIE, "", { path: "/", maxAge: 0 });
  return redirect;
}
