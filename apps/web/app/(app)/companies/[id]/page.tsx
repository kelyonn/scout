"use client";

import { use } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { ApiError, fetchCompanyDetail, fetchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { CompanyAvatar } from "@/components/CompanyAvatar";
import { WatchButton } from "@/components/WatchButton";
import { CardListSkeleton, EmptyState, ErrorNotice, Skeleton } from "@/components/ui";

export default function CompanyDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);

  const { data: company, isPending, isError, error, refetch } = useQuery({
    queryKey: ["company-detail", id],
    queryFn: () => fetchCompanyDetail(id),
  });

  const jobsQuery = useQuery({
    queryKey: ["jobs", "company", id],
    queryFn: () => fetchJobs({ minPriority: 1, companyId: id, limit: 50 }),
    enabled: !!company,
  });

  const jobs = jobsQuery.data?.data ?? [];
  const notFound = isError && error instanceof ApiError && error.isNotFound;

  return (
    <div className="max-w-5xl mx-auto page">
      <Link href="/companies" className="text-label text-text-muted hover:text-text-secondary transition-colors duration-100">
        ← Companies
      </Link>

      {isPending && (
        <div className="mt-6 space-y-4" role="status" aria-label="Loading">
          <Skeleton className="h-12 w-1/2" />
          <Skeleton className="h-20" />
        </div>
      )}
      {notFound && (
        <div className="mt-6">
          <EmptyState title="Company not found" action={{ label: "Back to Companies", href: "/companies" }}>
            It may have been merged into another entry or removed.
          </EmptyState>
        </div>
      )}
      {isError && !notFound && (
        <div className="mt-6">
          <ErrorNotice error={error} fallback="Couldn't load this company." onRetry={() => void refetch()} />
        </div>
      )}

      {company && (
        <div className="mt-6 grid grid-cols-1 lg:grid-cols-[1fr_280px] gap-8">
          <div className="min-w-0">
            <div className="flex items-center gap-3 sm:gap-4">
              <CompanyAvatar id={company.id} name={company.name} size={48} />
              <div className="min-w-0 flex-1">
                <h1 className="text-title text-text-primary">{company.name}</h1>
                <p className="text-caption text-text-muted mt-0.5">
                  {[company.hq_city, company.hq_country].filter(Boolean).join(", ")}
                </p>
              </div>
              <WatchButton companyId={company.id} />
            </div>

            {company.description && <p className="text-body text-text-secondary mt-5">{company.description}</p>}

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

            <section className="mt-8" aria-labelledby="open-roles">
              <h2 id="open-roles" className="text-heading text-text-primary mb-3">
                Open roles ({jobsQuery.isPending ? "…" : jobs.length})
              </h2>
              {jobsQuery.isPending && <CardListSkeleton count={2} />}
              {jobsQuery.isError && (
                <ErrorNotice error={jobsQuery.error} fallback="Couldn't load this company's roles." onRetry={() => void jobsQuery.refetch()} />
              )}
              {jobsQuery.isSuccess && jobs.length === 0 && (
                <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-6 text-center">
                  No scored open roles at this company right now.
                </div>
              )}
              <div className="space-y-4">
                {jobs.map((job) => (
                  <JobCard key={job.job_group_id} job={job} />
                ))}
              </div>
            </section>
          </div>

          <div className="lg:sticky lg:top-8 lg:self-start space-y-4">
            <dl className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 space-y-2.5 text-body">
              <Row label="Type" value={company.company_type.replace(/_/g, " ")} />
              {company.size_bucket && <Row label="Size" value={company.size_bucket} />}
              {company.stage && <Row label="Stage" value={company.stage.replace(/_/g, " ")} />}
              {company.founded_year && <Row label="Founded" value={String(company.founded_year)} />}
              <Row label="Open roles" value={String(company.open_job_count)} />
            </dl>

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
    <div className="flex items-center justify-between gap-3">
      <dt className="text-text-secondary">{label}</dt>
      <dd className="text-text-primary text-caption text-right">{value}</dd>
    </div>
  );
}
