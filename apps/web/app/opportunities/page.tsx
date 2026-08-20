"use client";

import { useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { fetchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { Shell } from "@/components/Shell";
import { useLiveFeed } from "@/lib/useLiveFeed";

const SENIORITY_FILTER = ["internship", "new_grad", "entry", "unknown"];

export default function OpportunitiesPage() {
  const queryClient = useQueryClient();
  const [newSinceMount, setNewSinceMount] = useState(0);

  const { data, isLoading, isError, error, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["jobs", SENIORITY_FILTER],
      queryFn: ({ pageParam }: { pageParam?: string }) =>
        fetchJobs({ minPriority: 1, seniority: SENIORITY_FILTER, cursor: pageParam, limit: 30 }),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) =>
        lastPage.page.has_more ? lastPage.page.next_cursor : undefined,
    });

  // docs/04-api-design.md section 4.3's SSE stream. Deliberately doesn't
  // splice the new job straight into the list — reordering the feed out
  // from under someone mid-read is worse than a one-tap banner, and this
  // still means a genuinely new posting never requires a manual refresh
  // to discover.
  useLiveFeed(() => setNewSinceMount((n) => n + 1));

  const jobs = data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <Shell>
      <div className="max-w-3xl mx-auto px-6 py-8">
        <div className="mb-6">
          <h1 className="text-title text-text-primary">Opportunities</h1>
          <p className="text-caption text-text-muted mt-1">
            {isLoading ? "Loading…" : `${jobs.length} shown, ranked by priority`}
          </p>
        </div>

        {newSinceMount > 0 && (
          <button
            onClick={() => {
              setNewSinceMount(0);
              queryClient.invalidateQueries({ queryKey: ["jobs"] });
            }}
            className="w-full mb-4 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2 transition-colors duration-100 hover:bg-accent-hover"
          >
            {newSinceMount} new opportunit{newSinceMount === 1 ? "y" : "ies"} — refresh
          </button>
        )}

        {isError && (
          <div className="text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
            Failed to load jobs: {error instanceof Error ? error.message : "unknown error"}
          </div>
        )}

        {isLoading && (
          <div className="space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="h-28 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface animate-pulse"
              />
            ))}
          </div>
        )}

        {!isLoading && !isError && jobs.length === 0 && (
          <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
            No jobs match right now. This filters to internship / new-grad /
            unresolved seniority — check back after the next crawl, or adjust
            source coverage.
          </div>
        )}

        <div className="space-y-3">
          {jobs.map((job) => (
            <JobCard key={job.job_group_id} job={job} />
          ))}
        </div>

        {hasNextPage && (
          <button
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="mt-4 w-full text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface py-2.5 transition-colors duration-100 hover:border-border-strong hover:text-text-primary disabled:opacity-50"
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </button>
        )}
      </div>
    </Shell>
  );
}
