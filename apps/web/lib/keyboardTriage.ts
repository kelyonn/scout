// Keyboard triage for the Opportunities list (docs/12-frontend-ux.md
// section 4.4): j/k move, x select, s save, d dismiss, Enter open.

export type TriageKey = "next" | "prev" | "select" | "save" | "dismiss" | "open" | "clear";

const KEY_MAP: Record<string, TriageKey> = {
  j: "next",
  k: "prev",
  x: "select",
  s: "save",
  d: "dismiss",
  Enter: "open",
  Escape: "clear",
};

export function triageKey(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean }): TriageKey | null {
  if (e.ctrlKey || e.metaKey || e.altKey) return null;
  return KEY_MAP[e.key] ?? null;
}

// Keys typed into a field, or on a focused control that already owns them
// (Enter on a button or link), must not trigger list shortcuts.
export function isTypingTarget(el: { tagName?: string; isContentEditable?: boolean } | null): boolean {
  if (!el) return false;
  if (el.isContentEditable) return true;
  const tag = (el.tagName ?? "").toUpperCase();
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

export function isInteractiveTarget(el: { tagName?: string } | null): boolean {
  const tag = (el?.tagName ?? "").toUpperCase();
  return tag === "A" || tag === "BUTTON";
}

export function moveIndex(current: number, delta: 1 | -1, length: number): number {
  if (length === 0) return -1;
  if (current < 0) return delta === 1 ? 0 : length - 1;
  return Math.max(0, Math.min(length - 1, current + delta));
}
