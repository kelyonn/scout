import { describe, expect, it } from "vitest";
import { PIPELINE_COLUMNS, pipelineDropTarget, transitionPath } from "../stateMachine";
import { SWIPE_COMMIT_PX, SWIPE_MAX_PX, lockAxis, swipeAction, swipeOffset } from "../swipe";
import { isInteractiveTarget, isTypingTarget, moveIndex, triageKey } from "../keyboardTriage";

const noMods = { ctrlKey: false, metaKey: false, altKey: false };

describe("transitionPath", () => {
  it("chains through the hops the server requires", () => {
    expect(transitionPath("new", "saved")).toEqual(["viewed", "saved"]);
    expect(transitionPath("viewed", "saved")).toEqual(["saved"]);
    expect(transitionPath("new", "applied")).toEqual(["viewed", "saved", "applied"]);
    expect(transitionPath("viewed", "applied")).toEqual(["saved", "applied"]);
    expect(transitionPath("saved", "applied")).toEqual(["applied"]);
  });
  it("dismisses straight from new", () => {
    expect(transitionPath("new", "dismissed")).toEqual(["dismissed"]);
    expect(transitionPath("dismissed", "saved")).toEqual(["saved"]);
  });
});

describe("pipelineDropTarget", () => {
  it("allows only legal single hops into single-state columns", () => {
    expect(pipelineDropTarget("saved", "Applied")).toBe("applied");
    expect(pipelineDropTarget("applied", "Screening")).toBe("screening");
    expect(pipelineDropTarget("interviewing", "Offer")).toBe("offer");
    expect(pipelineDropTarget("applied", "Saved")).toBe("saved");
  });
  it("refuses skips, backwards moves and no-ops", () => {
    expect(pipelineDropTarget("saved", "Interviewing")).toBeNull();
    expect(pipelineDropTarget("offer", "Applied")).toBeNull();
    expect(pipelineDropTarget("applied", "Applied")).toBeNull();
  });
  it("never picks an Archive outcome on the user's behalf", () => {
    expect(pipelineDropTarget("applied", "Archive")).toBeNull();
    expect(pipelineDropTarget("saved", "Nonexistent")).toBeNull();
    expect(PIPELINE_COLUMNS.find((c) => c.label === "Archive")?.states.length).toBeGreaterThan(1);
  });
});

describe("swipe", () => {
  it("leaves vertical scrolls and tiny movements alone", () => {
    expect(lockAxis(3, 4)).toBe("undecided");
    expect(lockAxis(8, 40)).toBe("vertical");
    expect(lockAxis(30, 25)).toBe("vertical");
  });
  it("locks to horizontal only when clearly sideways", () => {
    expect(lockAxis(40, 5)).toBe("horizontal");
    expect(lockAxis(-40, 10)).toBe("horizontal");
  });
  it("maps distance to save / dismiss only past the threshold", () => {
    expect(swipeAction(SWIPE_COMMIT_PX - 1)).toBeNull();
    expect(swipeAction(SWIPE_COMMIT_PX)).toBe("save");
    expect(swipeAction(-SWIPE_COMMIT_PX)).toBe("dismiss");
    expect(swipeAction(0)).toBeNull();
  });
  it("clamps the visual offset", () => {
    expect(swipeOffset(1000)).toBe(SWIPE_MAX_PX);
    expect(swipeOffset(-1000)).toBe(-SWIPE_MAX_PX);
    expect(swipeOffset(20)).toBe(20);
  });
});

describe("keyboard triage", () => {
  it("maps j/k/x/s/d/Enter/Escape and ignores modified keys", () => {
    expect(triageKey({ key: "j", ...noMods })).toBe("next");
    expect(triageKey({ key: "k", ...noMods })).toBe("prev");
    expect(triageKey({ key: "x", ...noMods })).toBe("select");
    expect(triageKey({ key: "s", ...noMods })).toBe("save");
    expect(triageKey({ key: "d", ...noMods })).toBe("dismiss");
    expect(triageKey({ key: "Enter", ...noMods })).toBe("open");
    expect(triageKey({ key: "q", ...noMods })).toBeNull();
    expect(triageKey({ key: "s", ...noMods, metaKey: true })).toBeNull();
  });
  it("ignores keys typed into fields", () => {
    expect(isTypingTarget({ tagName: "INPUT" })).toBe(true);
    expect(isTypingTarget({ tagName: "textarea" })).toBe(true);
    expect(isTypingTarget({ tagName: "SELECT" })).toBe(true);
    expect(isTypingTarget({ tagName: "DIV", isContentEditable: true })).toBe(true);
    expect(isTypingTarget({ tagName: "BODY" })).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
    expect(isInteractiveTarget({ tagName: "A" })).toBe(true);
  });
  it("moves and clamps the cursor", () => {
    expect(moveIndex(-1, 1, 5)).toBe(0);
    expect(moveIndex(-1, -1, 5)).toBe(4);
    expect(moveIndex(4, 1, 5)).toBe(4);
    expect(moveIndex(0, -1, 5)).toBe(0);
    expect(moveIndex(2, 1, 5)).toBe(3);
    expect(moveIndex(0, 1, 0)).toBe(-1);
  });
});
