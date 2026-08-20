"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchApplications } from "@/lib/api";
import { Shell } from "@/components/Shell";
import { StateMenu } from "@/components/StateMenu";
import { PIPELINE_COLUMNS } from "@/lib/stateMachine";
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
  return `${capitalize(item.state)} ${days} days ago — no response. Follow up?`;
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function PipelineCard({ item }: { item: ApplicationItem }) {
  const nudge = staleNudge(item);
  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-3">
      <div className="flex items-start justify-between gap-2">
        <Link
          href={`/opportunities/${item.job_group_id}`}
          className="min-w-0 text-label text-text-primary hover:text-accent-primary transition-colors duration-100 truncate"
        >
          {item.title}
        </Link>
        <StateMenu groupId={item.job_group_id} state={item.state} />
      </div>
      <p className="text-caption text-text-secondary mt-0.5 truncate">{item.company.name}</p>
      <p className="text-micro text-text-muted mt-1.5">
        {daysSince(item.state_changed_at)}d in {item.state}
        {item.job_status !== "open" && <span> · listing {item.job_status}</span>}
      </p>
      {nudge && (
        <p className="text-micro text-warning mt-1.5 rounded-[var(--radius-input)] bg-warning/10 border border-warning/30 px-1.5 py-1">
          {nudge}
        </p>
      )}
    </div>
  );
}

export default function PipelinePage() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  const items = data?.data ?? [];

  return (
    <Shell>
      <div className="px-6 py-8">
        <div className="mb-6">
          <h1 className="text-title text-text-primary">Pipeline</h1>
          <p className="text-caption text-text-muted mt-1">
            {isLoading ? "Loading…" : `${items.length} tracked`}
          </p>
        </div>

        {isError && (
          <div className="text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
            Failed to load pipeline: {error instanceof Error ? error.message : "unknown error"}
          </div>
        )}

        {!isLoading && !isError && items.length === 0 && (
          <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center max-w-xl">
            Nothing tracked yet. Save a job from Opportunities to start your pipeline.
          </div>
        )}

        {!isLoading && items.length > 0 && (
          <div className="flex gap-4 overflow-x-auto pb-4">
            {PIPELINE_COLUMNS.map((col) => {
              const colItems = items
                .filter((it) => col.states.includes(it.state))
                .sort((a, b) => new Date(b.state_changed_at).getTime() - new Date(a.state_changed_at).getTime());
              return (
                <div key={col.label} className="w-72 shrink-0">
                  <div className="flex items-center justify-between mb-2 px-0.5">
                    <h2 className="text-label text-text-secondary">{col.label}</h2>
                    <span className="text-micro text-text-muted">{colItems.length}</span>
                  </div>
                  <div className="space-y-2">
                    {colItems.map((item) => (
                      <PipelineCard key={item.job_group_id} item={item} />
                    ))}
                    {colItems.length === 0 && (
                      <div className="text-micro text-text-muted rounded-[var(--radius-card)] border border-dashed border-border-subtle p-3 text-center">
                        Empty
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Shell>
  );
}
