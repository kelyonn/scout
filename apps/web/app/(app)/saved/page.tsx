"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchApplications } from "@/lib/api";
import { JobCard } from "@/components/JobCard";

export default function SavedPage() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["applications", "saved"],
    queryFn: () => fetchApplications(["saved"]),
  });

  const items = data?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto px-10 py-10">
      <div className="mb-8">
        <h1 className="text-title text-text-primary">Saved</h1>
        <p className="text-caption text-text-muted mt-1.5">
          {isLoading ? "Loading…" : `${items.length} saved`}
        </p>
      </div>

      {isError && (
        <div className="text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
          Failed to load: {error instanceof Error ? error.message : "unknown error"}
        </div>
      )}

      {!isLoading && !isError && items.length === 0 && (
        <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
          Nothing saved yet. Save a job from Opportunities to see it here.
        </div>
      )}

      <div className="space-y-4">
        {items.map((item) => (
          <JobCard key={item.job_group_id} job={item} />
        ))}
      </div>
    </div>
  );
}
