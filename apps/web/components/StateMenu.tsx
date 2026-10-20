"use client";

import { useState } from "react";
import { STATE_LABEL, nextStatesFrom } from "@/lib/stateMachine";
import { useJobActions } from "@/lib/useJobActions";
import type { ApplicationState } from "@/lib/types";

// docs/12-frontend-ux.md section 4.5: "keyboard-accessible alternative
// via a state menu on every card (required — drag-only interfaces are
// inaccessible)." A native <select> is that alternative: the Pipeline
// board's drag-and-drop is a convenience on top of it, and this is the way
// to move a card by keyboard, screen reader or touch, and the only way to
// reach Archive outcomes (rejected / withdrawn), which a column drop
// cannot name.
export function StateMenu({ groupId, state }: { groupId: string; state: ApplicationState }) {
  const actions = useJobActions();
  const [pending, setPending] = useState(false);

  const options = nextStatesFrom(state);

  return (
    <select
      aria-label={`Move job from ${STATE_LABEL[state]} to a different state`}
      value=""
      disabled={pending || options.length === 0}
      onChange={(e) => {
        const target = e.target.value as ApplicationState;
        if (target) {
          setPending(true);
          void actions.move(groupId, state, target).finally(() => setPending(false));
        }
        e.target.value = "";
      }}
      className="text-caption bg-bg-elevated border border-border-subtle rounded-[var(--radius-input)] px-1.5 py-1.5 sm:py-1 text-text-secondary disabled:opacity-40"
    >
      <option value="" disabled>
        {options.length === 0 ? "Final" : "Move to…"}
      </option>
      {options.map((s) => (
        <option key={s} value={s}>
          {STATE_LABEL[s]}
        </option>
      ))}
    </select>
  );
}
