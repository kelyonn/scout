"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, updateJobState } from "@/lib/api";
import { patchApplicationState, patchJobState } from "@/lib/jobCache";
import { STATE_LABEL, nextStatesFrom, transitionPath } from "@/lib/stateMachine";
import { useToast } from "@/components/Toast";
import type { ApplicationState } from "@/lib/types";

// Triage only ever acts on jobs that haven't entered the pipeline: Save is
// legal from new / viewed (and brings back a dismissed one), Dismiss from
// new / viewed / saved. Everything later is moved from Pipeline.
export const canSave = (s: ApplicationState) => s === "new" || s === "viewed" || s === "dismissed";
export const canDismiss = (s: ApplicationState) => s === "new" || s === "viewed" || s === "saved";

// The one place a job's state is changed from the UI. Used by the card's
// buttons, swipe gestures, keyboard triage and the Pipeline board, so the
// hop-chaining, optimistic update, rollback and toasts behave identically
// everywhere.
export function useJobActions() {
  const queryClient = useQueryClient();
  const { toast } = useToast();

  const invalidate = useCallback(
    (groupId: string) => {
      void queryClient.invalidateQueries({ queryKey: ["jobs"] });
      void queryClient.invalidateQueries({ queryKey: ["job-detail", groupId] });
      void queryClient.invalidateQueries({ queryKey: ["applications"] });
    },
    [queryClient],
  );

  // Chains through the intermediate states a single gesture crosses. Shows
  // the optimistic end state immediately; on any failure shows one toast and
  // refetches, which restores whatever the last committed hop left.
  // Returns whether the whole chain committed.
  const transition = useCallback(
    async (
      groupId: string,
      from: ApplicationState,
      target: ApplicationState,
      opts: { foundElsewhereFirst?: boolean; optimistic?: boolean; silentError?: boolean } = {},
    ): Promise<boolean> => {
      if (opts.optimistic !== false) {
        patchJobState(queryClient, groupId, target);
        patchApplicationState(queryClient, groupId, target);
      }
      try {
        for (const step of transitionPath(from, target)) {
          await updateJobState(groupId, {
            state: step,
            foundElsewhereFirst: step === target ? opts.foundElsewhereFirst : undefined,
          });
        }
        invalidate(groupId);
        return true;
      } catch (err) {
        invalidate(groupId);
        if (!opts.silentError) toast(errorMessage(err, "Couldn't update this job. Try again."), { kind: "error" });
        return false;
      }
    },
    [queryClient, invalidate, toast],
  );

  const save = useCallback(
    async (groupId: string, from: ApplicationState): Promise<boolean> => {
      if (!canSave(from)) return false;
      const ok = await transition(groupId, from, "saved");
      if (ok) {
        // Undo from `saved` can only go to dismissed (or applied) — the
        // state machine has no way back to new/viewed.
        toast("Saved", { kind: "success", action: { label: "Undo", onClick: () => void transition(groupId, "saved", "dismissed") } });
      }
      return ok;
    },
    [transition, toast],
  );

  const dismiss = useCallback(
    async (groupId: string, from: ApplicationState): Promise<boolean> => {
      if (!canDismiss(from)) return false;
      const ok = await transition(groupId, from, "dismissed");
      if (ok) {
        toast("Dismissed", { action: { label: "Undo", onClick: () => void transition(groupId, "dismissed", "saved") } });
      }
      return ok;
    },
    [transition, toast],
  );

  // Keyboard triage on a selection: one toast for the whole batch rather
  // than one per job. Jobs that can't take the action (already saved, ...)
  // are skipped silently; failures are counted in the toast.
  const bulk = useCallback(
    async (kind: "save" | "dismiss", jobs: { groupId: string; state: ApplicationState }[]): Promise<void> => {
      const target: ApplicationState = kind === "save" ? "saved" : "dismissed";
      const eligible = jobs.filter((j) => (kind === "save" ? canSave(j.state) : canDismiss(j.state)));
      if (eligible.length === 0) return;
      if (eligible.length === 1) {
        await (kind === "save" ? save : dismiss)(eligible[0].groupId, eligible[0].state);
        return;
      }
      const results = await Promise.all(eligible.map((j) => transition(j.groupId, j.state, target, { silentError: true })));
      const done = eligible.filter((_, i) => results[i]);
      const failed = eligible.length - done.length;
      const verb = kind === "save" ? "Saved" : "Dismissed";
      if (done.length > 0) {
        const back: ApplicationState = kind === "save" ? "dismissed" : "saved";
        toast(`${verb} ${done.length} jobs${failed ? `, ${failed} failed` : ""}`, {
          kind: failed ? "error" : "info",
          action: {
            label: "Undo",
            onClick: () => void Promise.all(done.map((j) => transition(j.groupId, target, back))),
          },
        });
      } else {
        toast("Couldn't update those jobs. Try again.", { kind: "error" });
      }
    },
    [transition, save, dismiss, toast],
  );

  // Pipeline: move one column. Only legal single hops are offered by the UI,
  // but the server remains the authority.
  const move = useCallback(
    async (groupId: string, from: ApplicationState, target: ApplicationState): Promise<boolean> => {
      if (!nextStatesFrom(from).includes(target)) return false;
      const ok = await transition(groupId, from, target);
      if (ok) toast(`Moved to ${STATE_LABEL[target]}`, { kind: "success" });
      return ok;
    },
    [transition, toast],
  );

  return { transition, save, dismiss, bulk, move };
}
