"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchApplications } from "@/lib/api";
import { StateMenu } from "@/components/StateMenu";
import { EmptyState, ErrorNotice, PageHeader, Skeleton } from "@/components/ui";
import { PIPELINE_COLUMNS, STATE_LABEL } from "@/lib/stateMachine";
import type { ApplicationItem } from "@/lib/types";

const STALE_DAYS = 14;

function daysSince(iso: string): number {
  return Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000);
}

// docs/12-frontend-ux.md section 4.5: "Cards stale in a state for more
// than 14 days surface a nudge."
function staleNudge(item: ApplicationItem): string | null {
  if (item.state !== "applied" && item.state !== "screening" && item.state !== "interviewing") return null;
  const days = daysSince(item.state_changed_at);
  if (days <= STALE_DAYS) return null;
  return `${STATE_LABEL[item.state]} ${days} days ago — no response. Follow up?`;
}

function PipelineCard({ item }: { item: ApplicationItem }) {
  const nudge = staleNudge(item);
  return (
    <article className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
      <div className="flex items-start justify-between gap-2">
        <h3 className="min-w-0 text-[13px] font-semibold text-text-primary truncate">
          <Link href={`/opportunities/${item.job_group_id}`} className="hover:text-accent-primary transition-colors duration-100">
            {item.title}
          </Link>
        </h3>
        <StateMenu groupId={item.job_group_id} state={item.state} />
      </div>
      <p className="text-caption text-text-muted mt-1 truncate">{item.company.name}</p>
      <p className="text-[10px] uppercase tracking-[0.03em] text-text-faint mt-2">
        {daysSince(item.state_changed_at)}d in {STATE_LABEL[item.state]}
        {item.job_status !== "open" && <span> · listing {item.job_status}</span>}
      </p>
      {item.notes && (
        <p className="text-caption text-text-secondary mt-2 line-clamp-2" title={item.notes}>
          {item.notes}
        </p>
      )}
      {nudge && (
        <p className="text-[11px] text-text-secondary mt-2 rounded-[5px] bg-bg-elevated border border-border-subtle px-2 py-[5px]">
          {nudge}
        </p>
      )}
    </article>
  );
}

export default function PipelinePage() {
  const { data, isPending, isSuccess, isError, error, refetch } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  const items = data?.data ?? [];

  return (
    <div className="page">
      <PageHeader title="Pipeline" subtitle={isPending ? "Loading…" : isError ? undefined : `${items.length} tracked`} />

      {isError && <ErrorNotice error={error} fallback="Couldn't load your pipeline." onRetry={() => void refetch()} />}

      {isPending && (
        <div className="flex gap-5 overflow-hidden" role="status" aria-label="Loading">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="w-72 sm:w-80 h-40 shrink-0" />
          ))}
        </div>
      )}

      {isSuccess && items.length === 0 && (
        <div className="max-w-xl">
          <EmptyState title="Nothing tracked yet" action={{ label: "Browse opportunities", href: "/opportunities" }}>
            Save a job and it lands here. Move it along as you apply, interview, and hear back.
          </EmptyState>
        </div>
      )}

      {!isPending && items.length > 0 && (
        // docs/12 section 6: horizontal snap-scroll between columns on a
        // phone. Negative margin lets the first and last column reach the
        // screen edge while the page keeps its gutter.
        <div className="flex gap-4 sm:gap-5 overflow-x-auto pb-4 -mx-4 px-4 sm:mx-0 sm:px-0 snap-x snap-mandatory sm:snap-none">
          {PIPELINE_COLUMNS.map((col) => {
            const colItems = items
              .filter((it) => col.states.includes(it.state))
              .sort((a, b) => new Date(b.state_changed_at).getTime() - new Date(a.state_changed_at).getTime());
            return (
              <section key={col.label} aria-label={col.label} className="w-[82vw] sm:w-80 shrink-0 snap-start">
                <div className="flex items-center justify-between mb-3 px-0.5">
                  <h2 className="text-label text-text-secondary">{col.label}</h2>
                  <span className="text-micro text-text-muted">{colItems.length}</span>
                </div>
                <div className="space-y-2.5">
                  {colItems.map((item) => (
                    <PipelineCard key={item.job_group_id} item={item} />
                  ))}
                  {colItems.length === 0 && (
                    <div className="text-micro text-text-muted rounded-[var(--radius-card)] border border-dashed border-border-subtle p-3 text-center">
                      Empty
                    </div>
                  )}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
