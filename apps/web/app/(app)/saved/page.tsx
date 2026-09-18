"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchApplications } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { CardListSkeleton, EmptyState, ErrorNotice, PageHeader } from "@/components/ui";

export default function SavedPage() {
  const { data, isPending, isSuccess, isError, error, refetch } = useQuery({
    queryKey: ["applications", "saved"],
    queryFn: () => fetchApplications(["saved"]),
  });

  const items = data?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader title="Saved" subtitle={isPending ? "Loading…" : isError ? undefined : `${items.length} saved`} />

      {isError && <ErrorNotice error={error} fallback="Couldn't load saved jobs." onRetry={() => void refetch()} />}
      {isPending && <CardListSkeleton />}

      {isSuccess && items.length === 0 && (
        <EmptyState title="Nothing saved yet" action={{ label: "Browse opportunities", href: "/opportunities" }}>
          Save a job from the feed to keep it here, then apply when you&rsquo;re ready.
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
