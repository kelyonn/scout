"use client";

import { useState } from "react";
import { useToast } from "@/components/Toast";
import { useJobActions } from "@/lib/useJobActions";
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
  const { toast } = useToast();
  const actions = useJobActions();
  const [showElsewherePrompt, setShowElsewherePrompt] = useState(false);
  const [pending, setPending] = useState(false);

  // Each click is one user gesture, possibly several server hops (new →
  // viewed → saved → applied); see useJobActions.transition. A failure
  // shows one toast and the invalidated queries re-render the server's row.
  async function transitionTo(target: ApplicationState, foundElsewhereFirst?: boolean): Promise<boolean> {
    if (pending) return false;
    setPending(true);
    try {
      return await actions.transition(groupId, state, target, { foundElsewhereFirst });
    } finally {
      setPending(false);
    }
  }

  async function dismiss() {
    if (await transitionTo("dismissed")) {
      toast("Dismissed", {
        action: { label: "Undo", onClick: () => void actions.transition(groupId, "dismissed", "saved") },
      });
    }
  }

  async function apply(foundElsewhereFirst: boolean) {
    // Open the listing first, synchronously inside the click — popup
    // blockers only allow window.open during a user gesture, and an
    // await before it would lose that.
    window.open(applyUrl, "_blank", "noopener,noreferrer");
    setShowElsewherePrompt(false);
    if (await transitionTo("applied", foundElsewhereFirst)) {
      toast("Marked as applied", {
        kind: "success",
        action: { label: "Undo", onClick: () => void actions.transition(groupId, "applied", "saved") },
      });
    }
  }

  const pad = size === "sm" ? "px-2.5 py-1.5 sm:py-1" : "px-3 py-1.5";
  const text = size === "sm" ? "text-caption" : "text-label";
  const busy = pending;
  const base = `${text} ${pad} rounded-[var(--radius-input)] transition-colors duration-100 disabled:opacity-50 disabled:cursor-not-allowed`;
  const secondary = `${base} border border-border-subtle text-text-secondary hover:text-text-primary hover:border-border-strong`;
  const muted = `${base} border border-border-subtle text-text-muted hover:text-text-primary hover:border-border-strong`;
  const primary = `${base} text-white bg-accent-primary hover:bg-accent-hover`;

  if (state === "dismissed") {
    return (
      <button type="button" onClick={() => void transitionTo("saved")} disabled={busy} className={muted}>
        Undo dismiss
      </button>
    );
  }

  return (
    <div className="flex flex-wrap items-center justify-end gap-2" aria-busy={busy || undefined}>
      {/* Save / Unsave toggle. Same target state either way (a fresh
          `saved` from new/viewed, or back to `dismissed` once already
          saved — the one transition saved allows besides applying) — the
          button just flips which half of the pair is visible so the two
          read as one on/off control rather than two separate actions. */}
      {state === "saved" ? (
        <button
          type="button"
          onClick={() => void transitionTo("dismissed")}
          disabled={busy}
          title="Remove from Saved"
          aria-pressed
          className={`${base} border border-accent-primary/30 text-accent-primary bg-accent-subtle hover:bg-danger/10 hover:border-danger/30 hover:text-danger`}
        >
          Unsave
        </button>
      ) : (
        state !== "applied" && (
          <button type="button" onClick={() => void transitionTo("saved")} disabled={busy} aria-pressed={false} className={secondary}>
            Save
          </button>
        )
      )}

      {state !== "applied" && state !== "saved" && (
        <button type="button" onClick={() => void dismiss()} disabled={busy} className={muted}>
          Dismiss
        </button>
      )}

      {state === "applied" ? (
        <div className="flex items-center gap-1.5">
          <span className={`${text} ${pad} rounded-[var(--radius-input)] text-success`}>Applied</span>
          <button
            type="button"
            onClick={() => void transitionTo("saved")}
            disabled={busy}
            title="Didn't actually submit an application — move this back to Saved"
            className={muted}
          >
            Un-apply
          </button>
        </div>
      ) : showElsewherePrompt ? (
        <div role="group" aria-label="Had you already seen this listing?" className="flex flex-wrap items-center gap-1.5">
          <button type="button" onClick={() => void apply(false)} disabled={busy} className={primary}>
            Apply now →
          </button>
          <button
            type="button"
            onClick={() => void apply(true)}
            disabled={busy}
            title="I already knew about this one from elsewhere before Scout showed it to me"
            className={secondary}
          >
            Found elsewhere first
          </button>
          <button
            type="button"
            onClick={() => setShowElsewherePrompt(false)}
            aria-label="Cancel"
            className={`${base} text-text-muted hover:text-text-primary`}
          >
            ✕
          </button>
        </div>
      ) : (
        <button type="button" onClick={() => setShowElsewherePrompt(true)} disabled={busy} className={primary}>
          Apply →
        </button>
      )}
    </div>
  );
}
