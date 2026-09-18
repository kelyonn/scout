import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NOT_CONFIGURED, UPSTREAM_UNREACHABLE, proxyToApi } from "../proxy";

const API = "http://api.internal:8080";
const TOKEN = "test-token-0123456789abcdef0123456789abcdef";

describe("proxyToApi", () => {
  const original = { url: process.env.SCOUT_API_URL, token: process.env.SCOUT_AUTH_TOKEN };
  beforeEach(() => {
    process.env.SCOUT_API_URL = API;
    process.env.SCOUT_AUTH_TOKEN = TOKEN;
  });
  afterEach(() => {
    process.env.SCOUT_API_URL = original.url;
    process.env.SCOUT_AUTH_TOKEN = original.token;
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("forwards the path, query string, and bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{"data":[]}', { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const req = new Request("http://web.local/api/jobs?limit=5");
    const res = await proxyToApi(req, "/v1/jobs", { search: "?limit=5" });

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ data: [] });
    const [url, init] = fetchMock.mock.calls[0] as [URL, RequestInit];
    expect(url.toString()).toBe(`${API}/v1/jobs?limit=5`);
    expect((init.headers as Record<string, string>).Authorization).toBe(`Bearer ${TOKEN}`);
    expect(init.signal).toBe(req.signal);
  });

  it("answers 502 JSON when the upstream is unreachable, instead of throwing", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("fetch failed")));
    vi.spyOn(console, "error").mockImplementation(() => {});

    const res = await proxyToApi(new Request("http://web.local/api/jobs"), "/v1/jobs");

    expect(res.status).toBe(502);
    expect(res.headers.get("Content-Type")).toBe("application/json");
    expect(await res.json()).toEqual({ error: UPSTREAM_UNREACHABLE });
  });

  it("answers 500 JSON when the app itself is misconfigured", async () => {
    process.env.SCOUT_AUTH_TOKEN = "";
    const res = await proxyToApi(new Request("http://web.local/api/jobs"), "/v1/jobs");
    expect(res.status).toBe(500);
    expect(await res.json()).toEqual({ error: NOT_CONFIGURED });
  });

  it("passes upstream error statuses and bodies through unchanged", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"error":"job not found"}', { status: 404 })));
    const res = await proxyToApi(new Request("http://web.local/api/jobs/x"), "/v1/jobs/x");
    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ error: "job not found" });
  });

  it("returns a bodiless 204 without throwing", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    const res = await proxyToApi(new Request("http://web.local/api/watchlist", { method: "POST" }), "/v1/me/watchlist", {
      method: "POST",
      body: "{}",
      contentType: "application/json",
    });
    expect(res.status).toBe(204);
    expect(res.body).toBeNull();
  });

  it("forwards a POST body and content type", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await proxyToApi(new Request("http://web.local/x", { method: "POST" }), "/v1/x", {
      method: "POST",
      body: '{"state":"saved"}',
      contentType: "application/json",
    });
    const [, init] = fetchMock.mock.calls[0] as [URL, RequestInit];
    expect(init.method).toBe("POST");
    expect(init.body).toBe('{"state":"saved"}');
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
  });
});
