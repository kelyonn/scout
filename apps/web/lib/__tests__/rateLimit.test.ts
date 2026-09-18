import { describe, expect, it } from "vitest";
import { RateLimiter, clientKey } from "../rateLimit";

describe("RateLimiter", () => {
  it("allows up to the limit, then refuses until the window resets", () => {
    let now = 1_000_000;
    const limiter = new RateLimiter(3, 60_000, () => now);

    expect(limiter.hit("a").ok).toBe(true);
    expect(limiter.hit("a").ok).toBe(true);
    expect(limiter.hit("a").ok).toBe(true);

    const refused = limiter.hit("a");
    expect(refused.ok).toBe(false);
    expect(refused.retryAfterSeconds).toBeGreaterThan(0);
    expect(refused.retryAfterSeconds).toBeLessThanOrEqual(60);

    now += 60_001;
    expect(limiter.hit("a").ok).toBe(true);
  });

  it("keeps keys independent", () => {
    const limiter = new RateLimiter(1, 60_000, () => 0);
    expect(limiter.hit("a").ok).toBe(true);
    expect(limiter.hit("a").ok).toBe(false);
    expect(limiter.hit("b").ok).toBe(true);
  });

  it("reset clears a key's counter", () => {
    const limiter = new RateLimiter(1, 60_000, () => 0);
    limiter.hit("a");
    expect(limiter.hit("a").ok).toBe(false);
    limiter.reset("a");
    expect(limiter.hit("a").ok).toBe(true);
  });
});

describe("clientKey", () => {
  it("prefers the first X-Forwarded-For hop, then X-Real-IP, then a shared bucket", () => {
    expect(clientKey(new Request("http://x/", { headers: { "x-forwarded-for": "10.1.1.1, 10.2.2.2" } }))).toBe("10.1.1.1");
    expect(clientKey(new Request("http://x/", { headers: { "x-real-ip": "10.3.3.3" } }))).toBe("10.3.3.3");
    expect(clientKey(new Request("http://x/"))).toBe("shared");
  });
});
