import { randomBytes } from "crypto";
import { NextResponse } from "next/server";
import { apiConfig, jsonError, NOT_CONFIGURED, UPSTREAM_UNREACHABLE } from "@/lib/proxy";

const STATE_COOKIE = "scout_google_oauth_state";

// Starts the Google OAuth flow. Redirects the browser itself (not a
// fetch — this has to be a real navigation, since it ends on Google's
// own consent screen) to the authorize URL the Go API builds
// (apps/api/internal/googlecalendar.Handler.AuthURLHandler holds the
// actual client_id/scope construction; this route only adds the CSRF
// state and issues the redirect from Scout's own origin).
export async function GET(request: Request) {
  const config = apiConfig();
  if (!config) return jsonError(NOT_CONFIGURED, 500);

  const state = randomBytes(16).toString("hex");

  const upstream = new URL("/v1/integrations/google/auth-url", config.apiURL);
  upstream.searchParams.set("state", state);

  let res: Response;
  try {
    res = await fetch(upstream, {
      headers: { Authorization: `Bearer ${config.token}` },
      cache: "no-store",
      signal: request.signal,
    });
  } catch {
    return NextResponse.redirect(new URL("/settings?google=unreachable", request.url));
  }

  if (!res.ok) {
    // Not enabled on this deployment (no client id) or a transient
    // failure — either way, land on Settings with a readable state
    // rather than a bare JSON body in a full-page navigation.
    return NextResponse.redirect(new URL("/settings?google=error", request.url));
  }

  const { url } = (await res.json()) as { url: string };
  if (typeof url !== "string" || !url.startsWith("https://accounts.google.com/")) {
    console.error("google connect: unexpected authorize url host");
    return jsonError(UPSTREAM_UNREACHABLE, 502);
  }

  const redirect = NextResponse.redirect(url);
  // Short-lived — this cookie's only job is surviving the round trip to
  // Google's consent screen and back, checked once by the callback route
  // and never read again.
  redirect.cookies.set(STATE_COOKIE, state, {
    httpOnly: true,
    secure: process.env.SCOUT_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: 600,
  });
  return redirect;
}
