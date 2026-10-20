import { createHash, timingSafeEqual } from "crypto";

// Server-only. The browser-facing half of ADR-015's design (docs/adr/
// ADR-015-single-user-auth.md) — a single static bearer token, exchanged
// once for a long-lived cookie. apps/api/internal/auth already implements
// this on the Go side for direct API callers; this is the same mechanism
// for the Next.js origin, which is a separate origin from the API in local
// dev (no Caddy there) and needs its own gate rather than relying on a
// cookie the Go API set for a different host.
export const SESSION_COOKIE = "scout_web_session";

// Mirrors apps/api/internal/auth.Authenticator.valid exactly: hash both
// sides first so the comparison runs over two fixed-length 32-byte
// digests. timingSafeEqual throws on mismatched lengths rather than
// returning false, which raw (unhashed) tokens of different lengths would
// hit — hashing first avoids that entirely, on top of closing the timing
// leak.
export function isValidToken(presented: string): boolean {
  const expected = process.env.SCOUT_AUTH_TOKEN;
  if (!expected || !presented) return false;
  const got = createHash("sha256").update(presented).digest();
  const want = createHash("sha256").update(expected).digest();
  return timingSafeEqual(got, want);
}

// Optional convenience for a single-user install: SCOUT_LOGIN_PASSCODE (set
// in apps/web/.env.local, never committed) is a short code that can be
// typed in place of the 32+ character token. It is exchanged for the real
// token server-side, so the cookie and every API call still carry the long
// secret and the Go API's minimum-length rule is untouched. The login
// route's rate limit (10 wrong tries per 15 minutes per client) is what
// makes a short code acceptable.
export function resolveLoginToken(presented: string): string | null {
  if (isValidToken(presented)) return presented;
  const passcode = process.env.SCOUT_LOGIN_PASSCODE;
  const real = process.env.SCOUT_AUTH_TOKEN;
  if (!passcode || !real || !presented) return null;
  const got = createHash("sha256").update(presented).digest();
  const want = createHash("sha256").update(passcode).digest();
  return timingSafeEqual(got, want) ? real : null;
}

// One definition of the cookie's attributes, shared by login (set) and
// logout (clear) — a clear whose attributes differ from the set is
// silently ignored by the browser, which is how "Sign out" stops working.
//
// Secure is keyed on SCOUT_ENV, not NODE_ENV: `next build`/`next start`
// set NODE_ENV=production unconditionally, in the local Compose stack
// too, and a Secure cookie issued over plain HTTP is never stored — every
// login would appear to succeed and then immediately not be signed in.
// SCOUT_ENV is the same variable apps/api/cmd/main.go uses for exactly
// this local-vs-production distinction.
export function sessionCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.SCOUT_ENV === "production",
    sameSite: "strict" as const,
    path: "/",
    // One year. Rotation is the revocation mechanism (ADR-015), not
    // expiry — a short lifetime would mean re-typing the token on every
    // device on a schedule, which buys nothing when the same secret is
    // what gets re-typed.
    maxAge: 365 * 24 * 60 * 60,
  };
}
