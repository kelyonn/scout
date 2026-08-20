"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { updateJobState } from "@/lib/api";
import { STATE_LABEL, nextStatesFrom } from "@/lib/stateMachine";
import type { ApplicationState } from "@/lib/types";

// docs/12-frontend-ux.md section 4.5: "keyboard-accessible alternative
// via a state menu on every card (required — drag-only interfaces are
// inaccessible)." A native <select> is that alternative — no drag
// interaction is implemented in this pass, so this is the only way to
// move a card between Pipeline columns, not merely a fallback.
export function StateMenu({ groupId, state }: { groupId: string; state: ApplicationState }) {
  const queryClient = useQueryClient();
  const mutate = useMutation({
    mutationFn: (target: ApplicationState) => updateJobState(groupId, { state: target }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });

  const options = nextStatesFrom(state);

  return (
    <select
      aria-label={`Move job from ${STATE_LABEL[state]} to a different state`}
      value=""
      disabled={mutate.isPending || options.length === 0}
      onChange={(e) => {
        const target = e.target.value as ApplicationState;
        if (target) mutate.mutate(target);
        e.target.value = "";
      }}
      onClick={(e) => e.stopPropagation()}
      className="text-caption bg-bg-elevated border border-border-subtle rounded-[var(--radius-input)] px-1.5 py-1 text-text-secondary disabled:opacity-40"
    >
      <option value="" disabled>
        Move to…
      </option>
      {options.map((s) => (
        <option key={s} value={s}>
          {STATE_LABEL[s]}
        </option>
      ))}
    </select>
  );
}
