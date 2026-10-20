import { describe, expect, it } from "vitest";
import { safeNextPath } from "../safeRedirect";

describe("safeNextPath", () => {
  it("keeps an ordinary same-origin path, with its query string", () => {
    expect(safeNextPath("/opportunities?seniority=internship")).toBe("/opportunities?seniority=internship");
    expect(safeNextPath("/companies/abc")).toBe("/companies/abc");
  });

  it("falls back for empty input", () => {
    expect(safeNextPath(null)).toBe("/");
    expect(safeNextPath(undefined)).toBe("/");
    expect(safeNextPath("")).toBe("/");
    expect(safeNextPath("", "/pipeline")).toBe("/pipeline");
  });

  it("refuses anything that would leave the origin", () => {
    // The open-redirect shapes: absolute, protocol-relative, backslash
    // normalization, and header-injection characters.
    expect(safeNextPath("https://evil.example/")).toBe("/");
    expect(safeNextPath("//evil.example")).toBe("/");
    expect(safeNextPath("/\\evil.example")).toBe("/");
    expect(safeNextPath("/ok\r\nSet-Cookie: x")).toBe("/");
    expect(safeNextPath("javascript:alert(1)")).toBe("/");
    expect(safeNextPath("opportunities")).toBe("/");
  });

  it("never bounces back into the login page", () => {
    expect(safeNextPath("/login")).toBe("/");
    expect(safeNextPath("/login?next=/x")).toBe("/");
    expect(safeNextPath("/login/")).toBe("/");
    // A path that merely starts with the word is fine.
    expect(safeNextPath("/loginsomething")).toBe("/loginsomething");
  });
});
