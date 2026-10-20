import type { ApplicationState } from "./types";

// Mirrors apps/api/internal/jobs/state.go's validTransitions exactly —
// the frontend needs its own copy to know which next-states to offer in
// a menu without a round trip, but the server is what actually enforces
// this (a stale or tampered client can only ever get a 409 back, never a
// silent bad write).
export const VALID_TRANSITIONS: Record<ApplicationState, ApplicationState[]> = {
  new: ["viewed", "dismissed"],
  viewed: ["saved", "dismissed"],
  saved: ["applied", "dismissed", "withdrawn"],
  dismissed: ["saved"],
  applied: ["saved", "screening", "rejected", "withdrawn"],
  screening: ["interviewing", "rejected", "withdrawn"],
  interviewing: ["offer", "rejected", "withdrawn"],
  offer: ["accepted", "rejected", "withdrawn"],
  accepted: [],
  rejected: [],
  withdrawn: [],
  expired: [],
};

export const STATE_LABEL: Record<ApplicationState, string> = {
  new: "New",
  viewed: "Viewed",
  saved: "Saved",
  dismissed: "Dismissed",
  applied: "Applied",
  screening: "Screening",
  interviewing: "Interviewing",
  offer: "Offer",
  accepted: "Accepted",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
  expired: "Expired",
};

// docs/12-frontend-ux.md section 4.5's Pipeline columns, in display
// order. Archive bundles every terminal-or-inactive state into one
// collapsed column rather than four separate one-card columns.
export const PIPELINE_COLUMNS: { label: string; states: ApplicationState[] }[] = [
  { label: "Saved", states: ["saved"] },
  { label: "Applied", states: ["applied"] },
  { label: "Screening", states: ["screening"] },
  { label: "Interviewing", states: ["interviewing"] },
  { label: "Offer", states: ["offer"] },
  { label: "Archive", states: ["rejected", "withdrawn", "dismissed", "accepted", "expired"] },
];

export function nextStatesFrom(state: ApplicationState): ApplicationState[] {
  return VALID_TRANSITIONS[state] ?? [];
}

// The hops a single user gesture has to cross. A card first seen in `new`
// can't jump straight to `saved` or `applied`: the server validates every
// hop (apps/api/internal/jobs/state.go), so Save from `new` goes through
// `viewed`, and Apply goes through `viewed` and `saved`. Dismiss from `new`
// is a direct hop.
export function transitionPath(current: ApplicationState, target: ApplicationState): ApplicationState[] {
  const path: ApplicationState[] = [];
  if (current === "new" && (target === "saved" || target === "applied")) path.push("viewed");
  if ((current === "new" || current === "viewed") && target === "applied") path.push("saved");
  path.push(target);
  return path;
}

// Where a card dropped on a Pipeline column lands, or null when the drop
// isn't a legal single hop. Archive is deliberately not a drop target: it
// bundles rejected / withdrawn / dismissed / ..., and picking one of them
// for the user on a drag would silently record the wrong outcome — the
// state menu names the exact outcome instead.
export function pipelineDropTarget(from: ApplicationState, columnLabel: string): ApplicationState | null {
  const col = PIPELINE_COLUMNS.find((c) => c.label === columnLabel);
  if (!col || col.states.length !== 1) return null;
  const target = col.states[0];
  return nextStatesFrom(from).includes(target) ? target : null;
}
