"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchApplications } from "@/lib/api";
import { JobCard } from "@/components/JobCard";

// docs/19-roadmap.md's P3 scope named "Saved, Applied, and Search views"
// as separate deliverables; only Search and the Pipeline kanban board got
// built. This and app/(app)/saved/page.tsx are the plain-list versions —
// same JobCard component as Opportunities, filtered server-side by state
// via the applications endpoint Pipeline already uses.
export default function AppliedPage() {
  // Deliberately just "applied", not the whole in-progress funnel
  // (screening/interviewing/offer) — JobCard's collapsed applied
  // treatment (and its Undo control) is built for exactly that one
  // state; screening/interviewing/offer belong on the Pipeline board,
  // which already renders them with a state-appropriate move-to menu
  // instead of a Save/Dismiss/Apply row that wouldn't make sense there.
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["applications", "applied"],
    queryFn: () => fetchApplications(["applied"]),
  });

  const items = data?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto px-10 py-10">
      <div className="mb-8">
        <h1 className="text-title text-text-primary">Applied</h1>
        <p className="text-caption text-text-muted mt-1.5">
          {isLoading ? "Loading…" : `${items.length} in progress`}
        </p>
      </div>

      {isError && (
        <div className="text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
          Failed to load: {error instanceof Error ? error.message : "unknown error"}
        </div>
      )}

      {!isLoading && !isError && items.length === 0 && (
        <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
          Nothing applied yet.
        </div>
      )}

      <div className="space-y-4">
        {items.map((item) => (
          <JobCard key={item.job_group_id} job={item} />
        ))}
      </div>
    </div>
  );
}
