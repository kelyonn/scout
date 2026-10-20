import { NextResponse } from "next/server";
import { SESSION_COOKIE, resolveLoginToken, sessionCookieOptions } from "@/lib/auth";
import { RateLimiter, clientKey } from "@/lib/rateLimit";

// Exchanges the token for a long-lived session cookie — the one flow
// proxy.ts leaves open (see its matcher) so a browser with no cookie yet
// can reach this endpoint at all. The token itself is never stored
// anywhere new: the cookie's value is the token (same "no session store"
// reasoning as apps/api/internal/auth.SessionHandler — a session table
// would be state living somewhere durable, and there's exactly one user
// and one secret, so rotating the variable already is the revocation
// mechanism).

// Ten wrong tokens in fifteen minutes from one client, then 429s. A
// 32+ character token isn't brute-forceable over HTTP anyway; this
// exists so the attempt is loud and slow rather than silent and fast.
const limiter = new RateLimiter(10, 15 * 60 * 1000);

// Bounded so a giant body can't be used to tie up the process.
const MAX_TOKEN_LENGTH = 512;

export async function POST(request: Request) {
  const key = clientKey(request);
  const limit = limiter.hit(key);
  if (!limit.ok) {
    return NextResponse.json(
      { error: "too many attempts" },
      { status: 429, headers: { "Retry-After": String(limit.retryAfterSeconds) } },
    );
  }

  const body: unknown = await request.json().catch(() => null);
  const token =
    body && typeof body === "object" && "token" in body && typeof body.token === "string" ? body.token : "";

  const sessionToken = token.length === 0 || token.length > MAX_TOKEN_LENGTH ? null : resolveLoginToken(token);
  if (!sessionToken) {
    return NextResponse.json({ error: "invalid token" }, { status: 401 });
  }

  limiter.reset(key);
  const res = NextResponse.json({ status: "ok" });
  res.cookies.set(SESSION_COOKIE, sessionToken, sessionCookieOptions());
  return res;
}
