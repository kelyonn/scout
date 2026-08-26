"use client";

import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchApplications, fetchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { FilterBar } from "@/components/FilterBar";
import { useLiveFeed } from "@/lib/useLiveFeed";
import { useJobFilters, type JobFilters } from "@/lib/useJobFilters";
import type { ApplicationItem, JobFeedItem } from "@/lib/types";

// The starting filter this page always used before FilterBar existed —
// seeded into the URL on first visit (useJobFilters.seedDefaults), not
// hardcoded into the fetch itself, so it's a starting point the user can
// see and change, not an invisible floor they can't get past.
const DEFAULT_FILTERS: JobFilters = {
  seniority: ["internship", "new_grad", "entry", "unknown"],
  roleFamily: [],
  workMode: [],
  locationTier: [],
  minPriority: 1,
};

function isNewToday(job: JobFeedItem): boolean {
  const iso = job.first_seen_at ?? job.posted_at;
  if (!iso) return false;
  return Date.now() - new Date(iso).getTime() < 86_400_000;
}

// A real-data glance strip, not decoration — every number here comes
// from what's already been fetched for this page (jobs) or the same
// applications query Pipeline uses (shared react-query cache, so
// visiting Pipeline first costs nothing extra here).
function StatsStrip({ jobs, applications }: { jobs: JobFeedItem[]; applications: ApplicationItem[] }) {
  const newToday = jobs.filter(isNewToday).length;
  const saved = applications.filter((a) => a.state === "saved").length;
  const applied = applications.filter((a) => a.state === "applied").length;
  const inProgress = applications.filter((a) => a.state === "screening" || a.state === "interviewing").length;

  const tiles = [
    { label: "New today", value: newToday },
    { label: "Saved", value: saved },
    { label: "Applied", value: applied },
    { label: "In progress", value: inProgress },
  ];

  return (
    <div className="grid grid-cols-4 gap-3 mb-8">
      {tiles.map((t) => (
        <div key={t.label} className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-5 py-4">
          <div className="text-2xl font-semibold font-mono tabular-nums text-text-primary">{t.value}</div>
          <div className="text-caption text-text-muted mt-1">{t.label}</div>
        </div>
      ))}
    </div>
  );
}

export default function OpportunitiesPage() {
  const queryClient = useQueryClient();
  const [newSinceMount, setNewSinceMount] = useState(0);
  const { filters, hasAnyFilterParam, setFilters, seedDefaults } = useJobFilters();

  useEffect(() => {
    seedDefaults(DEFAULT_FILTERS);
    // seedDefaults itself is stable-enough (memoized on searchParams) but
    // only needs to run once per mount — re-running on every searchParams
    // change would fight the user's own edits by re-checking "is a filter
    // param present" against a URL that already has one because they just
    // set it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Fetches with DEFAULT_FILTERS for the one render before seedDefaults'
  // effect above has written them into the URL, rather than fetching
  // unfiltered first and refetching once the URL catches up — same data
  // either way, one fewer network round trip and no flash of an
  // unfiltered (much larger) result set.
  const effectiveFilters = hasAnyFilterParam ? filters : DEFAULT_FILTERS;

  const { data, isLoading, isError, error, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["jobs", effectiveFilters],
      queryFn: ({ pageParam }: { pageParam?: string }) =>
        fetchJobs({
          minPriority: effectiveFilters.minPriority ?? undefined,
          seniority: effectiveFilters.seniority,
          roleFamily: effectiveFilters.roleFamily,
          workMode: effectiveFilters.workMode,
          locationTier: effectiveFilters.locationTier,
          cursor: pageParam,
          limit: 30,
        }),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) =>
        lastPage.page.has_more ? lastPage.page.next_cursor : undefined,
    });

  const { data: applicationsData } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  // docs/04-api-design.md section 4.3's SSE stream. Deliberately doesn't
  // splice the new job straight into the list — reordering the feed out
  // from under someone mid-read is worse than a one-tap banner, and this
  // still means a genuinely new posting never requires a manual refresh
  // to discover.
  useLiveFeed(() => setNewSinceMount((n) => n + 1));

  const jobs = data?.pages.flatMap((p) => p.data) ?? [];
  const applications = applicationsData?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto px-10 py-10">
        <div className="mb-8">
          <h1 className="text-title text-text-primary">Opportunities</h1>
          <p className="text-caption text-text-muted mt-1.5">
            {isLoading ? "Loading…" : `${jobs.length} shown, ranked by priority`}
          </p>
        </div>

        {!isLoading && !isError && jobs.length > 0 && <StatsStrip jobs={jobs} applications={applications} />}

        <FilterBar filters={effectiveFilters} onChange={setFilters} resultCount={isLoading ? undefined : jobs.length} />

        {newSinceMount > 0 && (
          <button
            onClick={() => {
              setNewSinceMount(0);
              queryClient.invalidateQueries({ queryKey: ["jobs"] });
            }}
            className="w-full mb-5 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover"
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
          <div className="space-y-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="h-32 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface animate-pulse"
              />
            ))}
          </div>
        )}

        {!isLoading && !isError && jobs.length === 0 && (
          <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
            No jobs match the current filters — try widening Seniority or
            Location above, or Clear all.
          </div>
        )}

        <div className="space-y-4">
          {jobs.map((job) => (
            <JobCard key={job.job_group_id} job={job} />
          ))}
        </div>

        {hasNextPage && (
          <button
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="mt-5 w-full text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface py-3 transition-colors duration-100 hover:border-border-strong hover:text-text-primary disabled:opacity-50"
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </button>
        )}
    </div>
  );
}
