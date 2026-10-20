"use client";

import { Suspense, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { searchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { FilterBar } from "@/components/FilterBar";
import { CardListSkeleton, EmptyState, ErrorNotice, PageHeader } from "@/components/ui";
import { useJobFilters } from "@/lib/useJobFilters";

// GET /v1/search only accepts role_family/seniority/work_mode
// (packages/db/queries/search.sql) — no location_tier or min_priority,
// unlike the feed. Passed to FilterBar so it doesn't render controls
// that would silently do nothing here.
const SEARCH_FACETS = ["seniority", "workMode", "roleFamily"] as const;

// The query lives in the URL (?q=) alongside the filters, so a search is
// bookmarkable and survives a refresh or the back button — docs/12
// section 4.4's rule for filters, applied to the search term too.
function SearchContent() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const query = searchParams.get("q")?.trim() ?? "";
  const [input, setInput] = useState(query);
  const { filters, setFilters } = useJobFilters();

  function submit(next: string) {
    const params = new URLSearchParams(searchParams.toString());
    if (next) params.set("q", next);
    else params.delete("q");
    const qs = params.toString();
    router.push(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  }

  const { data, isPending, isSuccess, isError, error, isFetched, refetch } = useQuery({
    queryKey: ["search", query, filters],
    queryFn: () =>
      searchJobs(query, {
        seniority: filters.seniority,
        roleFamily: filters.roleFamily,
        workMode: filters.workMode,
      }),
    enabled: query.length > 0,
  });

  const results = data?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader title="Search" subtitle="Keyword search across titles, descriptions, and company names." />

      <form
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          submit(input.trim());
        }}
        className="flex gap-2 mb-6"
      >
        <label htmlFor="search-query" className="sr-only">
          Search jobs
        </label>
        <input
          id="search-query"
          type="search"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="e.g. backend intern bengaluru"
          enterKeyHint="search"
          autoFocus
          className="flex-1 min-w-0 text-body bg-bg-surface border border-border-subtle rounded-[var(--radius-card)] px-3 py-2 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary"
        />
        <button
          type="submit"
          className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2 transition-colors duration-100 hover:bg-accent-hover"
        >
          Search
        </button>
      </form>

      <FilterBar
        filters={filters}
        onChange={setFilters}
        facets={[...SEARCH_FACETS]}
        resultCount={isFetched && !isError ? results.length : undefined}
      />

      {isError && <ErrorNotice error={error} fallback="Search failed." onRetry={() => void refetch()} />}

      {isPending && <CardListSkeleton count={3} />}

      {!query && (
        <p className="text-caption text-text-muted">
          Try a role and a place (&ldquo;frontend intern remote&rdquo;), a technology (&ldquo;rust&rdquo;), or a company name.
        </p>
      )}

      {isFetched && isSuccess && results.length === 0 && (
        <EmptyState title={`No matches for “${query}”`}>Try fewer words, a different spelling, or clear the filters above.</EmptyState>
      )}

      {results.length > 0 && (
        <>
          <p className="text-caption text-text-muted mb-3">
            {results.length} result{results.length === 1 ? "" : "s"} for &ldquo;{query}&rdquo;
            {data?.mode_served && data.mode_served !== "keyword" ? "" : " (keyword match)"}
          </p>
          <div className="space-y-3">
            {results.map((job) => (
              <JobCard key={job.job_group_id} job={job} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}

export default function SearchPage() {
  return (
    <Suspense>
      <SearchContent />
    </Suspense>
  );
}
