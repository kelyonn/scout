"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { updateJobState } from "@/lib/api";
import type { ApplicationState } from "@/lib/types";

// docs/12-frontend-ux.md section 4.2's job-card action row: Save,
// Dismiss, Apply. Clicking Apply opens the "found elsewhere first"
// prompt (docs/16-observability.md section 2.1's "one tap ... is the
// whole instrument") before opening the real apply URL, rather than
// after — asking once, at the moment of leaving Scout for the listing,
// is the only point in the flow where the question is answerable
// honestly.
export function StateControls({
  groupId,
  state,
  applyUrl,
  size = "md",
}: {
  groupId: string;
  state: ApplicationState;
  applyUrl: string;
  size?: "sm" | "md";
}) {
  const queryClient = useQueryClient();
  const [showElsewherePrompt, setShowElsewherePrompt] = useState(false);
  const [pending, setPending] = useState(false);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["jobs"] });
    queryClient.invalidateQueries({ queryKey: ["job-detail", groupId] });
    queryClient.invalidateQueries({ queryKey: ["applications"] });
  };

  const mutate = useMutation({
    mutationFn: (params: { state: ApplicationState; foundElsewhereFirst?: boolean }) =>
      updateJobState(groupId, params),
    onSuccess: invalidate,
  });

  // Chains through the intermediate states a single click needs to
  // cross — the state machine (apps/api/internal/jobs/state.go) validates
  // every hop server-side regardless, but a card seen for the first time
  // is still in `new`, and "Save" or "Apply" from `new` needs to pass
  // through `viewed` (and `saved`, for Apply) first rather than 409ing on
  // a jump the user experiences as a single action.
  async function transitionTo(target: ApplicationState, foundElsewhereFirst?: boolean) {
    setPending(true);
    try {
      let current = state;
      const path: ApplicationState[] = [];
      if (current === "new" && (target === "saved" || target === "applied" || target === "dismissed")) {
        if (target !== "dismissed") path.push("viewed");
      }
      if ((current === "new" || current === "viewed") && target === "applied") {
        path.push("saved");
      }
      path.push(target);

      for (const step of path) {
        await mutate.mutateAsync({
          state: step,
          foundElsewhereFirst: step === target ? foundElsewhereFirst : undefined,
        });
        current = step;
      }
    } finally {
      setPending(false);
    }
  }

  const pad = size === "sm" ? "px-2.5 py-1" : "px-3 py-1.5";
  const text = size === "sm" ? "text-caption" : "text-label";
  const busy = pending || mutate.isPending;

  if (state === "dismissed") {
    return (
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          transitionTo("saved");
        }}
        disabled={busy}
        className={`${text} ${pad} rounded-[var(--radius-input)] border border-border-subtle text-text-muted transition-colors duration-100 hover:text-text-primary hover:border-border-strong disabled:opacity-50`}
      >
        Undo dismiss
      </button>
    );
  }

  return (
    <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
      {state !== "saved" && state !== "applied" && (
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault();
            transitionTo("saved");
          }}
          disabled={busy}
          className={`${text} ${pad} rounded-[var(--radius-input)] border border-border-subtle text-text-secondary transition-colors duration-100 hover:text-text-primary hover:border-border-strong disabled:opacity-50`}
        >
          Save
        </button>
      )}

      {state !== "applied" && (
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault();
            transitionTo("dismissed");
          }}
          disabled={busy}
          className={`${text} ${pad} rounded-[var(--radius-input)] border border-border-subtle text-text-muted transition-colors duration-100 hover:text-text-primary hover:border-border-strong disabled:opacity-50`}
        >
          Dismiss
        </button>
      )}

      {state === "applied" ? (
        <span className={`${text} ${pad} rounded-[var(--radius-input)] text-success flex items-center gap-1`}>
          ✓ Applied
        </span>
      ) : showElsewherePrompt ? (
        // Plain <button>s, not <a href>, deliberately: this component
        // renders inside JobCard's own outer <Link>, and a nested <a>
        // inside an <a> is invalid HTML with inconsistent click-target
        // behavior across browsers. window.open keeps "opens the real
        // listing in a new tab" without that nesting.
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              window.open(applyUrl, "_blank", "noopener,noreferrer");
              transitionTo("applied", false);
            }}
            className={`${text} ${pad} rounded-[var(--radius-input)] text-white bg-accent-primary transition-colors duration-100 hover:bg-accent-hover`}
          >
            Apply now →
          </button>
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              window.open(applyUrl, "_blank", "noopener,noreferrer");
              transitionTo("applied", true);
            }}
            title="I already knew about this one from elsewhere before Scout showed it to me"
            className={`${text} ${pad} rounded-[var(--radius-input)] border border-border-subtle text-text-secondary transition-colors duration-100 hover:text-text-primary`}
          >
            Found elsewhere first
          </button>
        </div>
      ) : (
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault();
            setShowElsewherePrompt(true);
          }}
          disabled={busy}
          className={`${text} ${pad} rounded-[var(--radius-input)] text-white bg-accent-primary transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50`}
        >
          Apply →
        </button>
      )}
    </div>
  );
}
