import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE, isValidToken } from "@/lib/auth";

// Exchanges the token for a long-lived session cookie — the one flow
// proxy.ts leaves open (see its matcher) so a browser with no cookie yet
// can reach this endpoint at all. The token itself is never stored
// anywhere new: the cookie's value is the token (same "no session store"
// reasoning as apps/api/internal/auth.SessionHandler — a session table
// would be state living somewhere durable, and there's exactly one user
// and one secret, so rotating the variable already is the revocation
// mechanism).
export async function POST(request: NextRequest) {
  const body = await request.json().catch(() => null);
  const token = typeof body?.token === "string" ? body.token : "";

  if (!isValidToken(token)) {
    return NextResponse.json({ error: "invalid token" }, { status: 401 });
  }

  const res = NextResponse.json({ status: "ok" });
  res.cookies.set(SESSION_COOKIE, token, {
    httpOnly: true,
    // SCOUT_ENV, not NODE_ENV — caught by actually running the built
    // standalone image (infra/docker/web.Dockerfile), not just `next
    // dev`. `next build`/`next start` set NODE_ENV=production
    // unconditionally, in both the local Compose stack and real
    // production, so checking it here would mark the cookie Secure
    // inside local Docker too, where there's no TLS — the browser would
    // silently refuse to store it and every login would appear to
    // succeed and then immediately not be signed in. SCOUT_ENV is the
    // same variable apps/api/cmd/main.go already uses for exactly this
    // local-vs-production distinction (see apps/api/internal/auth.go's
    // secureCookie field).
    secure: process.env.SCOUT_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: 365 * 24 * 60 * 60,
  });
  return res;
}
