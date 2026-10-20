import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { isValidToken, resolveLoginToken, sessionCookieOptions } from "../auth";

const TOKEN = "test-token-0123456789abcdef0123456789abcdef";

describe("isValidToken", () => {
  const original = { token: process.env.SCOUT_AUTH_TOKEN, env: process.env.SCOUT_ENV };
  beforeEach(() => {
    process.env.SCOUT_AUTH_TOKEN = TOKEN;
  });
  afterEach(() => {
    process.env.SCOUT_AUTH_TOKEN = original.token;
    process.env.SCOUT_ENV = original.env;
  });

  it("accepts exactly the configured token", () => {
    expect(isValidToken(TOKEN)).toBe(true);
  });

  it("rejects near misses, prefixes, and different lengths", () => {
    expect(isValidToken(TOKEN.slice(0, -1))).toBe(false);
    expect(isValidToken(TOKEN + "x")).toBe(false);
    expect(isValidToken(TOKEN.toUpperCase())).toBe(false);
    expect(isValidToken("")).toBe(false);
  });

  it("fails closed when no token is configured", () => {
    // An empty/missing SCOUT_AUTH_TOKEN must never make an empty
    // presented token valid — same rule as the Go authenticator.
    process.env.SCOUT_AUTH_TOKEN = "";
    expect(isValidToken("")).toBe(false);
    expect(isValidToken("anything")).toBe(false);
  });

  it("marks the cookie Secure only in production", () => {
    process.env.SCOUT_ENV = "local";
    expect(sessionCookieOptions().secure).toBe(false);
    process.env.SCOUT_ENV = "production";
    expect(sessionCookieOptions().secure).toBe(true);
    expect(sessionCookieOptions().httpOnly).toBe(true);
    expect(sessionCookieOptions().sameSite).toBe("strict");
  });
});

describe("resolveLoginToken", () => {
  const original = {
    token: process.env.SCOUT_AUTH_TOKEN,
    passcode: process.env.SCOUT_LOGIN_PASSCODE,
  };
  beforeEach(() => {
    process.env.SCOUT_AUTH_TOKEN = TOKEN;
    process.env.SCOUT_LOGIN_PASSCODE = "246810";
  });
  afterEach(() => {
    process.env.SCOUT_AUTH_TOKEN = original.token;
    process.env.SCOUT_LOGIN_PASSCODE = original.passcode;
  });

  it("returns the real token for the real token or the passcode", () => {
    expect(resolveLoginToken(TOKEN)).toBe(TOKEN);
    expect(resolveLoginToken("246810")).toBe(TOKEN);
  });

  it("rejects anything else, and the passcode when none is configured", () => {
    expect(resolveLoginToken("246811")).toBeNull();
    expect(resolveLoginToken("")).toBeNull();
    process.env.SCOUT_LOGIN_PASSCODE = "";
    expect(resolveLoginToken("246810")).toBeNull();
  });
});
