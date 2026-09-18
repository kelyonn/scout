// Server-only. A fixed-window counter in process memory, for the one
// endpoint in this app that accepts a credential (app/api/auth/login).
//
// This is deliberately not Redis-backed: Redis holds nothing durable
// (AGENTS.md rule 8) and this isn't durable either — it just needs to
// make a brute-force attempt against a 32+ character token slower than
// the network already makes it. The web app runs as a single Node
// process (infra/docker/web.Dockerfile), so one Map is the whole state.
// A restart resets the counters, which is fine: the goal is throttling,
// not a permanent lockout the user could trip themselves on.

interface Window {
  count: number;
  resetAt: number;
}

export interface RateLimitResult {
  ok: boolean;
  /** Seconds until the window resets — only meaningful when !ok. */
  retryAfterSeconds: number;
}

export class RateLimiter {
  private readonly windows = new Map<string, Window>();

  constructor(
    private readonly limit: number,
    private readonly windowMs: number,
    private readonly now: () => number = Date.now,
  ) {}

  /** Records one attempt for `key` and reports whether it is allowed. */
  hit(key: string): RateLimitResult {
    const now = this.now();
    this.evict(now);

    const current = this.windows.get(key);
    if (!current || current.resetAt <= now) {
      this.windows.set(key, { count: 1, resetAt: now + this.windowMs });
      return { ok: true, retryAfterSeconds: 0 };
    }

    current.count += 1;
    if (current.count > this.limit) {
      return { ok: false, retryAfterSeconds: Math.max(1, Math.ceil((current.resetAt - now) / 1000)) };
    }
    return { ok: true, retryAfterSeconds: 0 };
  }

  /** Clears the counter for `key` — called after a successful sign-in so
   * a legitimate typo earlier in the window doesn't linger. */
  reset(key: string): void {
    this.windows.delete(key);
  }

  // Bounded memory: expired windows are dropped on every hit rather than
  // on a timer, so there's nothing to leak and nothing to unschedule.
  private evict(now: number): void {
    if (this.windows.size < 1024) return;
    for (const [key, w] of this.windows) {
      if (w.resetAt <= now) this.windows.delete(key);
    }
  }
}

/** Best-effort client key for a request: the first X-Forwarded-For hop
 * (Caddy sets it in production), else X-Real-IP, else a shared bucket.
 * A shared bucket is the safe failure mode for a single-user app — it
 * throttles everyone together rather than nobody. */
export function clientKey(request: Request): string {
  const forwarded = request.headers.get("x-forwarded-for");
  if (forwarded) {
    const first = forwarded.split(",")[0]?.trim();
    if (first) return first;
  }
  return request.headers.get("x-real-ip") ?? "shared";
}
