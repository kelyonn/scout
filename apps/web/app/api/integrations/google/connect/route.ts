import { randomBytes } from "crypto";
import { NextResponse } from "next/server";

const STATE_COOKIE = "scout_google_oauth_state";

// Starts the Google OAuth flow. Redirects the browser itself (not a
// fetch — this has to be a real navigation, since it ends on Google's
// own consent screen) to the authorize URL the Go API builds
// (apps/api/internal/googlecalendar.Handler.AuthURLHandler holds the
// actual client_id/scope construction; this route only adds the CSRF
// state and issues the redirect from Scout's own origin).
export async function GET() {
  const apiURL = process.env.SCOUT_API_URL;
  const token = process.env.SCOUT_AUTH_TOKEN;
  if (!apiURL || !token) {
    return NextResponse.json(
      { error: "SCOUT_API_URL/SCOUT_AUTH_TOKEN not configured" },
      { status: 500 },
    );
  }

  const state = randomBytes(16).toString("hex");

  const upstream = new URL("/v1/integrations/google/auth-url", apiURL);
  upstream.searchParams.set("state", state);
  const res = await fetch(upstream, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });

  if (!res.ok) {
    const body = await res.text();
    return new NextResponse(body, { status: res.status, headers: { "Content-Type": "application/json" } });
  }

  const { url } = (await res.json()) as { url: string };
  const redirect = NextResponse.redirect(url);
  // Short-lived — this cookie's only job is surviving the round trip to
  // Google's consent screen and back, checked once by the callback route
  // below and never read again.
  redirect.cookies.set(STATE_COOKIE, state, {
    httpOnly: true,
    secure: process.env.SCOUT_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: 600,
  });
  return redirect;
}
