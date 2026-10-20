// Pure gesture classification for the job-card swipe (docs/12-frontend-ux.md
// section 6: right = save, left = dismiss). Kept free of DOM types so the
// thresholds are unit-testable.

/** Movement under this many px decides nothing yet. */
export const SWIPE_LOCK_PX = 10;
/** Horizontal travel needed to commit the action on release. */
export const SWIPE_COMMIT_PX = 88;
/** How far the card visually follows the finger. */
export const SWIPE_MAX_PX = 140;

export type SwipeAxis = "undecided" | "horizontal" | "vertical";
export type SwipeAction = "save" | "dismiss";

// Horizontal only when the finger moves clearly more sideways than up/down —
// otherwise the gesture is a vertical scroll and must be left alone.
export function lockAxis(dx: number, dy: number): SwipeAxis {
  const ax = Math.abs(dx);
  const ay = Math.abs(dy);
  if (Math.max(ax, ay) < SWIPE_LOCK_PX) return "undecided";
  return ax > ay * 1.5 ? "horizontal" : "vertical";
}

export function swipeOffset(dx: number): number {
  return Math.max(-SWIPE_MAX_PX, Math.min(SWIPE_MAX_PX, dx));
}

export function swipeAction(dx: number): SwipeAction | null {
  if (dx >= SWIPE_COMMIT_PX) return "save";
  if (dx <= -SWIPE_COMMIT_PX) return "dismiss";
  return null;
}
