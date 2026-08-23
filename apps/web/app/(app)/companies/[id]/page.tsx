"use client";

import { use } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchCompanyDetail, fetchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { CompanyAvatar } from "@/components/CompanyAvatar";
import { WatchButton } from "@/components/WatchButton";

export default function CompanyDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);

  const { data: company, isLoading, isError, error } = useQuery({
    queryKey: ["company-detail", id],
    queryFn: () => fetchCompanyDetail(id),
  });

  const { data: jobsData, isLoading: jobsLoading } = useQuery({
    queryKey: ["jobs", "company", id],
    queryFn: () => fetchJobs({ minPriority: 1, companyId: id, limit: 50 }),
  });

  const jobs = jobsData?.data ?? [];

  return (
    <div className="max-w-5xl mx-auto px-10 py-10">
      <Link
        href="/companies"
        className="text-label text-text-muted hover:text-text-secondary transition-colors duration-100"
      >
        ← Companies
      </Link>

      {isLoading && <p className="text-body text-text-muted mt-6">Loading…</p>}
      {isError && (
        <div className="mt-6 text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
          {error instanceof Error ? error.message : "Failed to load company"}
        </div>
      )}

      {company && (
        <div className="mt-6 grid grid-cols-1 lg:grid-cols-[1fr_280px] gap-8">
          <div className="min-w-0">
            <div className="flex items-center gap-4">
              <CompanyAvatar id={company.id} name={company.name} size={48} />
              <div className="min-w-0 flex-1">
                <h1 className="text-title text-text-primary">{company.name}</h1>
                <p className="text-caption text-text-muted mt-0.5">
                  {[company.hq_city, company.hq_country].filter(Boolean).join(", ")}
                </p>
              </div>
              <WatchButton companyId={company.id} />
            </div>

            {company.description && (
              <p className="text-body text-text-secondary mt-5">{company.description}</p>
            )}

            {company.industries.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-4">
                {company.industries.map((ind) => (
                  <span
                    key={ind}
                    className="text-micro px-1.5 py-0.5 rounded-[var(--radius-input)] bg-bg-elevated text-text-muted border border-border-subtle"
                  >
                    {ind.replace(/_/g, " ")}
                  </span>
                ))}
              </div>
            )}

            <div className="mt-8">
              <h2 className="text-heading text-text-primary mb-3">
                Open roles ({jobsLoading ? "…" : jobs.length})
              </h2>
              {jobsLoading && <p className="text-body text-text-muted">Loading…</p>}
              {!jobsLoading && jobs.length === 0 && (
                <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-6 text-center">
                  Nothing currently open matches the feed&rsquo;s seniority filter.
                </div>
              )}
              <div className="space-y-4">
                {jobs.map((job) => (
                  <JobCard key={job.job_group_id} job={job} />
                ))}
              </div>
            </div>
          </div>

          <div className="lg:sticky lg:top-8 lg:self-start space-y-4">
            <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 space-y-2.5 text-body">
              <Row label="Type" value={company.company_type.replace(/_/g, " ")} />
              {company.size_bucket && <Row label="Size" value={company.size_bucket} />}
              {company.stage && <Row label="Stage" value={company.stage.replace(/_/g, " ")} />}
              {company.founded_year && <Row label="Founded" value={String(company.founded_year)} />}
              <Row label="Open roles" value={String(company.open_job_count)} />
            </div>

            {company.website_url && (
              <a
                href={company.website_url}
                target="_blank"
                rel="noopener noreferrer"
                className="block text-center text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface py-2.5 transition-colors duration-100 hover:border-border-strong hover:text-text-primary"
              >
                Visit website ↗
              </a>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary text-caption">{value}</span>
    </div>
  );
}
