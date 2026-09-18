"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchApplications } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { CardListSkeleton, EmptyState, ErrorNotice, PageHeader } from "@/components/ui";

// Deliberately just "applied", not the whole in-progress funnel
// (screening/interviewing/offer) — JobCard's collapsed applied
// treatment (and its Undo control) is built for exactly that one
// state; screening/interviewing/offer belong on the Pipeline board.
export default function AppliedPage() {
  const { data, isPending, isSuccess, isError, error, refetch } = useQuery({
    queryKey: ["applications", "applied"],
    queryFn: () => fetchApplications(["applied"]),
  });

  const items = data?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Applied"
        subtitle={isPending ? "Loading…" : isError ? undefined : `${items.length} awaiting a response — move them along in Pipeline once you hear back`}
      />

      {isError && <ErrorNotice error={error} fallback="Couldn't load applied jobs." onRetry={() => void refetch()} />}
      {isPending && <CardListSkeleton />}

      {isSuccess && items.length === 0 && (
        <EmptyState title="Nothing applied yet" action={{ label: "See saved jobs", href: "/saved" }}>
          When you hit Apply on a job, it shows up here until you move it to Screening, Rejected, or Withdrawn.
        </EmptyState>
      )}

      <div className="space-y-4">
        {items.map((item) => (
          <JobCard key={item.job_group_id} job={item} />
        ))}
      </div>
    </div>
  );
}
