"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchApplications, fetchJobs } from "@/lib/api";
import { EmptyState, ErrorNotice, PageHeader, Skeleton } from "@/components/ui";
import { formatRoleFamily } from "@/lib/taxonomy";

function Bar({ label, count, total, color }: { label: string; count: number; total: number; color: string }) {
  const pct = total > 0 ? (count / total) * 100 : 0;
  return (
    <div className="flex items-center gap-3">
      <span className="text-caption text-text-secondary w-28 sm:w-36 shrink-0 truncate" title={label}>
        {label}
      </span>
      <div className="flex-1 h-2 rounded-full bg-bg-elevated overflow-hidden">
        <div className="h-full rounded-full transition-[width] duration-300" style={{ width: `${pct}%`, background: color }} />
      </div>
      <span className="text-caption font-mono tabular-nums text-text-muted w-8 text-right">{count}</span>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5">
      <h2 className="text-heading text-text-primary mb-4">{title}</h2>
      {children}
    </div>
  );
}

function topN<T extends string>(counts: Record<T, number>, n: number): [T, number][] {
  return (Object.entries(counts) as [T, number][]).sort((a, b) => b[1] - a[1]).slice(0, n);
}

// Every number here is computed client-side from the same two endpoints
// (jobs feed, applications) every other page already calls — no new
// aggregation backend. A richer version (trends over time, calibrated
// against a real corpus) is explicitly P5 scope in docs/19-roadmap.md;
// this is the honest subset that's free with data already in hand.
export default function InsightsPage() {
  const jobsQuery = useQuery({
    queryKey: ["jobs", "insights"],
    queryFn: () => fetchJobs({ minPriority: 1, limit: 100 }),
  });
  const applicationsQuery = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });
  const { data: jobsData, isPending: jobsLoading } = jobsQuery;
  const { data: applicationsData, isPending: applicationsLoading } = applicationsQuery;
  const failed = jobsQuery.isError ? jobsQuery : applicationsQuery.isError ? applicationsQuery : null;

  const jobs = jobsData?.data ?? [];
  const applications = applicationsData?.data ?? [];
  const loading = jobsLoading || applicationsLoading;

  const scoreBands = { "90-100": 0, "80-89": 0, "70-79": 0, "60-69": 0, "<60": 0 };
  for (const j of jobs) {
    if (j.priority >= 90) scoreBands["90-100"]++;
    else if (j.priority >= 80) scoreBands["80-89"]++;
    else if (j.priority >= 70) scoreBands["70-79"]++;
    else if (j.priority >= 60) scoreBands["60-69"]++;
    else scoreBands["<60"]++;
  }

  const roleFamilies: Record<string, number> = {};
  for (const j of jobs) roleFamilies[j.role_family] = (roleFamilies[j.role_family] ?? 0) + 1;

  const workModes: Record<string, number> = {};
  for (const j of jobs) workModes[j.location.work_mode] = (workModes[j.location.work_mode] ?? 0) + 1;

  const funnel = [
    { label: "Saved", count: applications.filter((a) => a.state === "saved").length },
    { label: "Applied", count: applications.filter((a) => a.state === "applied").length },
    { label: "Screening", count: applications.filter((a) => a.state === "screening").length },
    { label: "Interviewing", count: applications.filter((a) => a.state === "interviewing").length },
    { label: "Offer", count: applications.filter((a) => a.state === "offer").length },
  ];
  const funnelMax = Math.max(1, ...funnel.map((f) => f.count));

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Insights"
        subtitle={loading ? "Loading…" : `Computed from the top ${jobs.length} jobs in your feed and everything in your pipeline.`}
      />

      {failed && <ErrorNotice error={failed.error} fallback="Couldn't load insights." onRetry={() => void failed.refetch()} />}

      {loading && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4" role="status" aria-label="Loading">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-48" />
          ))}
        </div>
      )}

      {!loading && !failed && jobs.length === 0 && applications.length === 0 && (
        <EmptyState title="Nothing to chart yet" action={{ label: "Browse opportunities", href: "/opportunities" }}>
          Insights fill in once the feed has jobs and you&rsquo;ve started saving or applying.
        </EmptyState>
      )}

      {!loading && !failed && (jobs.length > 0 || applications.length > 0) && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Panel title="Score distribution">
            <div className="space-y-3">
              {(Object.entries(scoreBands) as [string, number][]).map(([label, count]) => (
                <Bar key={label} label={label} count={count} total={jobs.length} color="var(--accent-primary)" />
              ))}
            </div>
          </Panel>

          <Panel title="Your funnel">
            <div className="space-y-3">
              {funnel.map((f) => (
                <Bar key={f.label} label={f.label} count={f.count} total={funnelMax} color="var(--info)" />
              ))}
            </div>
          </Panel>

          <Panel title="Top role families">
            <div className="space-y-3">
              {topN(roleFamilies, 6).map(([label, count]) => (
                <Bar key={label} label={formatRoleFamily(label)} count={count} total={jobs.length} color="var(--success)" />
              ))}
            </div>
          </Panel>

          <Panel title="Work mode">
            <div className="space-y-3">
              {topN(workModes, 6).map(([label, count]) => (
                <Bar key={label} label={label} count={count} total={jobs.length} color="var(--warning)" />
              ))}
            </div>
          </Panel>
        </div>
      )}
    </div>
  );
}
