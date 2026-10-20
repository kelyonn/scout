"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchApplications, fetchJobs } from "@/lib/api";
import { VirtualJobList } from "@/components/VirtualJobList";
import { FilterBar } from "@/components/FilterBar";
import { CardListSkeleton, EmptyState, ErrorNotice, PageHeader } from "@/components/ui";
import { useLiveFeed } from "@/lib/useLiveFeed";
import { isInteractiveTarget, isTypingTarget, moveIndex, triageKey } from "@/lib/keyboardTriage";
import { useJobActions } from "@/lib/useJobActions";
import { useJobFilters, type JobFilters } from "@/lib/useJobFilters";
import type { ApplicationItem, JobFeedItem } from "@/lib/types";

// The starting filter this page always used before FilterBar existed —
// seeded into the URL on first visit (useJobFilters.seedDefaults), not
// hardcoded into the fetch itself, so it's a starting point the user can
// see and change, not an invisible floor they can't get past. No
// seniority restriction: Scout collects mid/senior/staff roles too, and
// hiding them by default silently threw away real, already-ranked
// results — narrowing to a seniority is the user's own choice via
// FilterBar, not a baked-in assumption about who's using this.
const DEFAULT_FILTERS: JobFilters = {
  seniority: [],
  roleFamily: [],
  workMode: [],
  locationTier: [],
  minPriority: 1,
};

const EMPTY: JobFilters = { seniority: [], roleFamily: [], workMode: [], locationTier: [], minPriority: null };

function isNewToday(job: JobFeedItem): boolean {
  const iso = job.first_seen_at ?? job.posted_at;
  if (!iso) return false;
  return Date.now() - new Date(iso).getTime() < 86_400_000;
}

// A real-data glance strip, not decoration — every number here comes
// from what's already been fetched for this page (jobs) or the same
// applications query Pipeline uses (shared react-query cache).
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
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6 sm:mb-8">
      {tiles.map((t) => (
        <div key={t.label} className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-4 py-3.5 sm:px-5 sm:py-4">
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
    // Only once per mount — re-running on every searchParams change would
    // fight the user's own edits.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Fetches with DEFAULT_FILTERS for the one render before seedDefaults'
  // effect above has written them into the URL, rather than fetching
  // unfiltered first and refetching once the URL catches up.
  const effectiveFilters = hasAnyFilterParam ? filters : DEFAULT_FILTERS;
  const filtersActive =
    effectiveFilters.seniority.length + effectiveFilters.roleFamily.length + effectiveFilters.workMode.length + effectiveFilters.locationTier.length > 0 ||
    effectiveFilters.minPriority !== null;

  const { data, isPending, isSuccess, isError, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
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
    getNextPageParam: (lastPage) => (lastPage.page.has_more ? lastPage.page.next_cursor : undefined),
  });

  const { data: applicationsData } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  // docs/04-api-design.md section 4.3's SSE stream. Deliberately doesn't
  // splice the new job straight into the list — reordering the feed out
  // from under someone mid-read is worse than a one-tap banner.
  useLiveFeed(() => setNewSinceMount((n) => n + 1));

  const jobs = useMemo(() => data?.pages.flatMap((p) => p.data) ?? [], [data]);
  const applications = applicationsData?.data ?? [];

  // docs/12-frontend-ux.md section 4.4 keyboard triage. State lives by job id,
  // not index, so a refetch or a new page never moves the cursor.
  const router = useRouter();
  const actions = useJobActions();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [scrollTo, setScrollTo] = useState<{ id: string; n: number } | null>(null);

  const onNearEnd = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.repeat) return;
      const key = triageKey(e);
      if (!key) return;
      const target = e.target as HTMLElement | null;
      if (isTypingTarget(target)) return;
      if ((key === "open" || key === "clear") && isInteractiveTarget(target)) return;
      if (key === "clear" && selected.size === 0) return;

      const idx = jobs.findIndex((j) => j.job_group_id === activeId);
      const active = idx >= 0 ? jobs[idx] : null;

      if (key === "next" || key === "prev") {
        const next = jobs[moveIndex(idx, key === "next" ? 1 : -1, jobs.length)];
        if (!next) return;
        e.preventDefault();
        setActiveId(next.job_group_id);
        setScrollTo((p) => ({ id: next.job_group_id, n: (p?.n ?? 0) + 1 }));
        return;
      }
      if (key === "clear") {
        setSelected(new Set());
        return;
      }
      if (!active && selected.size === 0) return;
      e.preventDefault();

      if (key === "select" && active) {
        setSelected((prev) => {
          const next = new Set(prev);
          if (!next.delete(active.job_group_id)) next.add(active.job_group_id);
          return next;
        });
      } else if (key === "open" && active) {
        router.push(`/opportunities/${active.job_group_id}`);
      } else if (key === "save" || key === "dismiss") {
        const targets = selected.size > 0 ? jobs.filter((j) => selected.has(j.job_group_id)) : active ? [active] : [];
        void actions.bulk(
          key,
          targets.map((j) => ({ groupId: j.job_group_id, state: j.state })),
        );
        setSelected(new Set());
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [jobs, activeId, selected, actions, router]);

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Opportunities"
        subtitle={isPending ? "Loading…" : isError ? undefined : `${jobs.length}${hasNextPage ? "+" : ""} shown, ranked by priority`}
      />

      {isSuccess && jobs.length > 0 && <StatsStrip jobs={jobs} applications={applications} />}

      <FilterBar filters={effectiveFilters} onChange={setFilters} resultCount={isPending ? undefined : jobs.length} />

      {newSinceMount > 0 && (
        <button
          type="button"
          onClick={() => {
            setNewSinceMount(0);
            queryClient.invalidateQueries({ queryKey: ["jobs"] });
          }}
          className="w-full mb-5 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover"
        >
          {newSinceMount} new opportunit{newSinceMount === 1 ? "y" : "ies"} — refresh
        </button>
      )}

      {isError && <ErrorNotice error={error} fallback="Couldn't load opportunities." onRetry={() => void refetch()} />}

      {isPending && <CardListSkeleton count={5} />}

      {isSuccess && jobs.length === 0 && (
        <EmptyState
          title={filtersActive ? "No jobs match these filters" : "Nothing collected yet"}
          action={
            filtersActive ? (
              <button
                type="button"
                onClick={() => setFilters(EMPTY)}
                className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2.5 transition-colors duration-100 hover:bg-accent-hover"
              >
                Clear all filters
              </button>
            ) : undefined
          }
        >
          {filtersActive
            ? "Try widening Seniority or Location above, or clear everything to see the whole feed."
            : "Once the collector has run, ranked postings appear here. Home has the setup steps."}
        </EmptyState>
      )}

      {selected.size > 0 && (
        <p role="status" className="mb-3 text-caption text-text-secondary">
          {selected.size} selected — <kbd className="font-mono">s</kbd> save · <kbd className="font-mono">d</kbd> dismiss · <kbd className="font-mono">Esc</kbd> clear
        </p>
      )}

      <VirtualJobList jobs={jobs} activeId={activeId} selectedIds={selected} scrollToId={scrollTo} onNearEnd={onNearEnd} />

      {jobs.length > 0 && (
        <p className="hidden md:block mt-4 text-micro text-text-faint" aria-hidden>
          j/k move · x select · s save · d dismiss · Enter open
        </p>
      )}

      {hasNextPage && (
        <button
          type="button"
          onClick={() => void fetchNextPage()}
          disabled={isFetchingNextPage}
          className="mt-5 w-full text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface py-3 transition-colors duration-100 hover:border-border-strong hover:text-text-primary disabled:opacity-50"
        >
          {isFetchingNextPage ? "Loading…" : "Load more"}
        </button>
      )}
    </div>
  );
}
