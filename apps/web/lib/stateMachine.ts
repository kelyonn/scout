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
  applied: ["screening", "rejected", "withdrawn"],
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
