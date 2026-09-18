"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { errorMessage, updateJobState } from "@/lib/api";
import { useToast } from "@/components/Toast";
import { STATE_LABEL, nextStatesFrom } from "@/lib/stateMachine";
import type { ApplicationState } from "@/lib/types";

// docs/12-frontend-ux.md section 4.5: "keyboard-accessible alternative
// via a state menu on every card (required — drag-only interfaces are
// inaccessible)." A native <select> is that alternative — no drag
// interaction is implemented, so this is the only way to move a card
// between Pipeline columns, not merely a fallback.
export function StateMenu({ groupId, state }: { groupId: string; state: ApplicationState }) {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const mutate = useMutation({
    mutationFn: (target: ApplicationState) => updateJobState(groupId, { state: target }),
    onSuccess: (_, target) => {
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
      queryClient.invalidateQueries({ queryKey: ["job-detail", groupId] });
      toast(`Moved to ${STATE_LABEL[target]}`, { kind: "success" });
    },
    onError: (err) => {
      toast(errorMessage(err, "Couldn't move this job. Try again."), { kind: "error" });
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
