"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { addToWatchlist, fetchWatchlist, removeFromWatchlist } from "@/lib/api";

// Watch/Unwatch toggle for the watchlist_hiring trigger (docs/11 section 2)
// — same on/off-pair styling as StateControls' Save/Unsave, so watchlist
// membership reads as the same kind of control rather than a one-off. The
// ["watchlist"] query is shared across every instance on a page (Companies'
// list renders one per row), so react-query dedupes it to a single request
// no matter how many buttons are on screen.
export function WatchButton({ companyId, size = "md" }: { companyId: string; size?: "sm" | "md" }) {
  const queryClient = useQueryClient();
  const { data } = useQuery({ queryKey: ["watchlist"], queryFn: fetchWatchlist });
  const watched = (data?.data ?? []).some((w) => w.company_id === companyId);

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["watchlist"] });
  const add = useMutation({ mutationFn: () => addToWatchlist(companyId), onSuccess: invalidate });
  const remove = useMutation({ mutationFn: () => removeFromWatchlist(companyId), onSuccess: invalidate });
  const busy = add.isPending || remove.isPending;

  const pad = size === "sm" ? "px-2.5 py-1" : "px-3 py-1.5";
  const text = size === "sm" ? "text-caption" : "text-label";

  if (watched) {
    return (
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          remove.mutate();
        }}
        disabled={busy}
        title="Stop watching — you'll no longer get watchlist_hiring notifications for this company"
        className={`${text} ${pad} rounded-[var(--radius-input)] border border-accent-primary/30 text-accent-primary bg-accent-subtle transition-colors duration-100 hover:bg-danger/10 hover:border-danger/30 hover:text-danger disabled:opacity-50`}
      >
        Watching
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        add.mutate();
      }}
      disabled={busy}
      title="Get notified whenever this company posts a relevant role"
      className={`${text} ${pad} rounded-[var(--radius-input)] border border-border-subtle text-text-secondary transition-colors duration-100 hover:text-text-primary hover:border-border-strong disabled:opacity-50`}
    >
      Watch
    </button>
  );
}
