"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import { JobCard } from "@/components/JobCard";
import type { JobFeedItem } from "@/lib/types";

const ESTIMATE_PX = 176;
const GAP_PX = 16;
// Ask for the next page once the viewport is this many rows from the end.
const PREFETCH_ROWS = 6;

// docs/12-frontend-ux.md section 4.4: a virtualized list so thousands of
// results scroll smoothly. The page scrolls (not an inner container), so
// the window virtualizer is used; row heights vary (collapsed dismissed and
// applied cards are much shorter), so each row is measured.
export function VirtualJobList({
  jobs,
  activeId,
  selectedIds,
  scrollToId,
  onNearEnd,
}: {
  jobs: JobFeedItem[];
  activeId: string | null;
  selectedIds: ReadonlySet<string>;
  /** Changes whenever keyboard navigation wants a row brought into view. */
  scrollToId: { id: string; n: number } | null;
  onNearEnd: () => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);
  const [scrollMargin, setScrollMargin] = useState(0);

  // The list starts below the header, stats and filters, whose height
  // changes (filters wrap, stats appear after load), so re-measure whenever
  // the page body resizes.
  useLayoutEffect(() => {
    const measure = () => {
      if (!listRef.current) return;
      const top = Math.round(listRef.current.getBoundingClientRect().top + window.scrollY);
      setScrollMargin((prev) => (prev === top ? prev : top));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(document.body);
    return () => ro.disconnect();
  }, []);

  const virtualizer = useWindowVirtualizer({
    count: jobs.length,
    estimateSize: () => ESTIMATE_PX,
    gap: GAP_PX,
    overscan: 6,
    scrollMargin,
    getItemKey: (i) => jobs[i]?.job_group_id ?? i,
  });

  const items = virtualizer.getVirtualItems();
  const lastIndex = items.length > 0 ? items[items.length - 1].index : -1;
  useLayoutEffect(() => {
    if (lastIndex >= jobs.length - PREFETCH_ROWS) onNearEnd();
  }, [lastIndex, jobs.length, onNearEnd]);

  useLayoutEffect(() => {
    if (!scrollToId) return;
    const idx = jobs.findIndex((j) => j.job_group_id === scrollToId.id);
    if (idx >= 0) virtualizer.scrollToIndex(idx, { align: "auto" });
    // Only when navigation asks (n changes), not when rows re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scrollToId]);

  return (
    <div ref={listRef} role="list" aria-label="Opportunities" style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
      {items.map((row) => {
        const job = jobs[row.index];
        if (!job) return null;
        const active = job.job_group_id === activeId;
        const selected = selectedIds.has(job.job_group_id);
        return (
          <div
            key={row.key}
            ref={virtualizer.measureElement}
            data-index={row.index}
            role="listitem"
            aria-current={active || undefined}
            data-selected={selected || undefined}
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              width: "100%",
              transform: `translateY(${row.start - virtualizer.options.scrollMargin}px)`,
            }}
          >
            <div
              className={`rounded-[var(--radius-card)] ${
                active ? "outline-2 outline-offset-2 outline-accent-primary" : selected ? "outline-2 outline-offset-2 outline-info" : ""
              }`}
            >
              {selected && (
                <span className="sr-only">Selected</span>
              )}
              <JobCard job={job} />
            </div>
          </div>
        );
      })}
    </div>
  );
}
