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
