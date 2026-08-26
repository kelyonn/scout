"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

// Filter param names match apps/api's own query params exactly
// (role_family, seniority, work_mode, location_tier, min_priority) —
// docs/04-api-design.md's GET /v1/jobs and GET /v1/search both accept
// these, so FilterBar's state doubles as the fetch params with no
// translation layer in between.
export interface JobFilters {
  seniority: string[];
  roleFamily: string[];
  workMode: string[];
  locationTier: string[];
  minPriority: number | null;
}

export const EMPTY_FILTERS: JobFilters = {
  seniority: [],
  roleFamily: [],
  workMode: [],
  locationTier: [],
  minPriority: null,
};

// useJobFilters reads filter state from the URL's query string and
// writes back to it — docs/12-frontend-ux.md section 4.4: "Filters are
// URL state, so any view is shareable and bookmarkable and survives a
// refresh." An absent key means "no filter on that facet" (the same
// null-means-unfiltered convention apps/api's sqlc.narg-based queries
// already use — packages/db/queries/feed.sql and search.sql), not "use
// some hidden default" — a caller that wants a default (Opportunities'
// own internship/new-grad/entry/unspecified starting filter) seeds it
// once via `router.replace`, after which the URL is the only source of
// truth.
export function useJobFilters(): {
  filters: JobFilters;
  /** True once the URL carries at least one filter key — false only in
   * the single-render window before a page's own seedDefaults effect has
   * run. Callers with a non-empty default (Opportunities) should compute
   * their *effective* filters as `hasAnyFilterParam ? filters : DEFAULT`
   * for fetching, rather than fetching unfiltered for one render and
   * refetching once the URL catches up. */
  hasAnyFilterParam: boolean;
  setFilters: (next: JobFilters) => void;
  seedDefaults: (defaults: JobFilters) => void;
} {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const hasAnyFilterParam =
    searchParams.has("seniority") ||
    searchParams.has("role_family") ||
    searchParams.has("work_mode") ||
    searchParams.has("location_tier") ||
    searchParams.has("min_priority");

  const filters = useMemo<JobFilters>(() => {
    const minPriorityRaw = searchParams.get("min_priority");
    return {
      seniority: searchParams.getAll("seniority"),
      roleFamily: searchParams.getAll("role_family"),
      workMode: searchParams.getAll("work_mode"),
      locationTier: searchParams.getAll("location_tier"),
      minPriority: minPriorityRaw ? Number(minPriorityRaw) : null,
    };
  }, [searchParams]);

  const writeFilters = useCallback(
    (next: JobFilters, mode: "push" | "replace") => {
      const params = new URLSearchParams(searchParams.toString());
      params.delete("seniority");
      params.delete("role_family");
      params.delete("work_mode");
      params.delete("location_tier");
      params.delete("min_priority");
      for (const v of next.seniority) params.append("seniority", v);
      for (const v of next.roleFamily) params.append("role_family", v);
      for (const v of next.workMode) params.append("work_mode", v);
      for (const v of next.locationTier) params.append("location_tier", v);
      if (next.minPriority !== null) params.set("min_priority", String(next.minPriority));
      // Filter changes reset pagination cursors — a cursor computed
      // against the old filtered set is meaningless against a new one.
      params.delete("cursor");

      const qs = params.toString();
      const url = qs ? `${pathname}?${qs}` : pathname;
      if (mode === "replace") router.replace(url, { scroll: false });
      else router.push(url, { scroll: false });
    },
    [pathname, router, searchParams],
  );

  const setFilters = useCallback((next: JobFilters) => writeFilters(next, "push"), [writeFilters]);

  // seedDefaults writes defaults into the URL only when no filter key is
  // present at all yet (first visit, no query string) — a caller that
  // has since cleared every checkbox on purpose gets an empty query
  // string, which this must not overwrite back to the default. Uses
  // "replace" so seeding a default doesn't add a throwaway history entry.
  const seedDefaults = useCallback(
    (defaults: JobFilters) => {
      if (!hasAnyFilterParam) writeFilters(defaults, "replace");
    },
    [hasAnyFilterParam, writeFilters],
  );

  return { filters, hasAnyFilterParam, setFilters, seedDefaults };
}
