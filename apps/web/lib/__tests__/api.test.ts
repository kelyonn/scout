import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, NETWORK_ERROR_MESSAGE, errorMessage, fetchJobDetail, updateJobState } from "../api";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("api client error mapping", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("turns a network failure into a readable ApiError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    await expect(fetchJobDetail("abc")).rejects.toMatchObject({
      name: "ApiError",
      status: 0,
      message: NETWORK_ERROR_MESSAGE,
    });
  });

  it("keeps the server's detail but shows a human message for a 409", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(409, { error: "invalid transition: saved -> applied" })));
    const err = await updateJobState("abc", { state: "applied" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.status).toBe(409);
    expect(apiErr.detail).toBe("invalid transition: saved -> applied");
    expect(apiErr.message).not.toContain("->");
    expect(apiErr.message).toMatch(/current state/i);
  });

  it("surfaces a 422's own reason, capitalized", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(422, { error: "rating must be between 1 and 5" })));
    const err = (await updateJobState("abc", { state: "viewed", rating: 9 }).catch((e: unknown) => e)) as ApiError;
    expect(err.message).toBe("Rating must be between 1 and 5.");
  });

  it("never renders an HTML error page as the message", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("<!DOCTYPE html><h1>Internal Server Error</h1>", { status: 500 })),
    );
    const err = (await fetchJobDetail("abc").catch((e: unknown) => e)) as ApiError;
    expect(err.status).toBe(500);
    expect(err.detail).toBeNull();
    expect(err.message).not.toContain("<");
  });

  it("flags a 404 so pages can render a not-found state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(404, { error: "job not found (or no longer open)" })));
    const err = (await fetchJobDetail("abc").catch((e: unknown) => e)) as ApiError;
    expect(err.isNotFound).toBe(true);
  });

  it("sends the state payload in the API's snake_case shape", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { job_group_id: "abc", state: "applied", state_changed_at: "", found_elsewhere_first: true }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await updateJobState("abc", { state: "applied", foundElsewhereFirst: true, notes: "hi" });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/jobs/abc/state");
    expect(JSON.parse(init.body as string)).toEqual({ state: "applied", found_elsewhere_first: true, notes: "hi" });
  });
});

describe("errorMessage", () => {
  it("prefers ApiError messages, then Error messages, then the fallback", () => {
    expect(errorMessage(new ApiError(500, "Server said no"))).toBe("Server said no");
    expect(errorMessage(new Error("boom"))).toBe("boom");
    expect(errorMessage("weird", "Fallback")).toBe("Fallback");
    expect(errorMessage(undefined)).toBe("Something went wrong. Try again.");
  });
});
