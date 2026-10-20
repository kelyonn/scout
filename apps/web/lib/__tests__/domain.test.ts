import { describe, expect, it } from "vitest";
import { jobDeepLinkPath } from "../capacitor";
import { PIPELINE_COLUMNS, VALID_TRANSITIONS, nextStatesFrom } from "../stateMachine";
import { ROLE_FAMILY_OPTIONS, formatRoleFamily } from "../taxonomy";
import type { ApplicationState } from "../types";

const ALL_STATES: ApplicationState[] = [
  "new",
  "viewed",
  "saved",
  "dismissed",
  "applied",
  "screening",
  "interviewing",
  "offer",
  "accepted",
  "rejected",
  "withdrawn",
  "expired",
];

describe("state machine (frontend mirror of apps/api/internal/jobs/state.go)", () => {
  it("covers every application state and only points at real states", () => {
    for (const s of ALL_STATES) {
      expect(VALID_TRANSITIONS[s]).toBeDefined();
      for (const target of VALID_TRANSITIONS[s]) expect(ALL_STATES).toContain(target);
    }
  });

  it("matches the server's transition table", () => {
    // Kept in lockstep with validTransitions in state.go by hand; this
    // pins the current shape so a one-sided edit fails here.
    expect(nextStatesFrom("new")).toEqual(["viewed", "dismissed"]);
    expect(nextStatesFrom("saved")).toEqual(["applied", "dismissed", "withdrawn"]);
    expect(nextStatesFrom("applied")).toEqual(["saved", "screening", "rejected", "withdrawn"]);
    expect(nextStatesFrom("offer")).toEqual(["accepted", "rejected", "withdrawn"]);
    expect(nextStatesFrom("accepted")).toEqual([]);
    expect(nextStatesFrom("expired")).toEqual([]);
  });

  it("puts every non-new/viewed state in exactly one pipeline column", () => {
    const seen = new Map<ApplicationState, number>();
    for (const col of PIPELINE_COLUMNS) for (const s of col.states) seen.set(s, (seen.get(s) ?? 0) + 1);
    for (const s of ALL_STATES) {
      if (s === "new" || s === "viewed") {
        expect(seen.has(s)).toBe(false);
      } else {
        expect(seen.get(s)).toBe(1);
      }
    }
  });
});

describe("formatRoleFamily", () => {
  it("names the family and drops redundant suffixes", () => {
    expect(formatRoleFamily("swe.general")).toBe("Software Engineering");
    expect(formatRoleFamily("swe.other")).toBe("Software Engineering");
    expect(formatRoleFamily("swe.infra.devops")).toBe("Software Engineering (Infra/Devops)");
    expect(formatRoleFamily("advocacy.devrel")).toBe("Advocacy (Devrel)");
    expect(formatRoleFamily("")).toBe("");
  });

  it("produces a label for every filter option", () => {
    for (const r of ROLE_FAMILY_OPTIONS) expect(formatRoleFamily(r)).not.toBe("");
  });
});

describe("jobDeepLinkPath", () => {
  it("maps the notifier's scout://job/{id} scheme to the detail route", () => {
    expect(jobDeepLinkPath("scout://job/abc-123")).toBe("/opportunities/abc-123");
    expect(jobDeepLinkPath("scout://job/abc?x=1")).toBe("/opportunities/abc");
  });

  it("ignores anything else", () => {
    expect(jobDeepLinkPath("scout://company/abc")).toBeNull();
    expect(jobDeepLinkPath("https://example.com/job/abc")).toBeNull();
    expect(jobDeepLinkPath("")).toBeNull();
  });
});
