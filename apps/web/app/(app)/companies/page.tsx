"use client";

import { Suspense, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchCompanies, fetchWatchlist } from "@/lib/api";
import { CompanyAvatar } from "@/components/CompanyAvatar";
import { WatchButton } from "@/components/WatchButton";
import { EmptyState, ErrorNotice, PageHeader, Skeleton } from "@/components/ui";

const SIZE_LABEL: Record<string, string> = {
  "1-10": "1-10",
  "11-50": "11-50",
  "51-200": "51-200",
  "201-500": "201-500",
  "501-1000": "501-1k",
  "1001-5000": "1k-5k",
  "5001-10000": "5k-10k",
  "10000+": "10k+",
};

function CompaniesContent() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const query = searchParams.get("q")?.trim() ?? "";
  const [input, setInput] = useState(query);

  function submit(next: string) {
    router.push(next ? `${pathname}?q=${encodeURIComponent(next)}` : pathname, { scroll: false });
  }

  const { data, isPending, isSuccess, isError, error, refetch } = useQuery({
    queryKey: ["companies", query],
    queryFn: () => fetchCompanies(query || undefined),
  });

  const { data: watchlistData } = useQuery({ queryKey: ["watchlist"], queryFn: fetchWatchlist });

  const companies = data?.data ?? [];
  const watchlist = watchlistData?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Companies"
        subtitle={isPending ? "Loading…" : isError ? undefined : `${companies.length} hiring right now${query ? ` matching “${query}”` : ""}`}
      />

      {watchlist.length > 0 && !query && (
        <section aria-labelledby="watchlist-heading" className="mb-8">
          <h2 id="watchlist-heading" className="text-heading text-text-primary mb-1">
            Your watchlist
          </h2>
          <p className="text-caption text-text-muted mb-3">You&rsquo;re notified whenever one of these posts a relevant role.</p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {watchlist.map((w) => (
              <CompanyRow
                key={w.company_id}
                id={w.company_id}
                name={w.company_name}
                meta={`${w.open_job_count} open role${w.open_job_count === 1 ? "" : "s"}`}
                highlighted
              />
            ))}
          </div>
        </section>
      )}

      <form
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          submit(input.trim());
        }}
        className="flex gap-2 mb-6"
      >
        <label htmlFor="company-query" className="sr-only">
          Search companies
        </label>
        <input
          id="company-query"
          type="search"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Search companies"
          enterKeyHint="search"
          className="flex-1 min-w-0 text-body bg-bg-surface border border-border-subtle rounded-[var(--radius-card)] px-3 py-2 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary"
        />
        <button
          type="submit"
          className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2 transition-colors duration-100 hover:bg-accent-hover"
        >
          Search
        </button>
      </form>

      {isError && <ErrorNotice error={error} fallback="Couldn't load companies." onRetry={() => void refetch()} />}

      {isPending && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3" role="status" aria-label="Loading">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-[72px]" />
          ))}
        </div>
      )}

      {isSuccess && companies.length === 0 && (
        <EmptyState
          title={query ? `No companies match “${query}”` : "No companies with open roles yet"}
          action={
            query ? (
              <button
                type="button"
                onClick={() => {
                  setInput("");
                  submit("");
                }}
                className="text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle px-4 py-2.5 transition-colors duration-100 hover:border-border-strong hover:text-text-primary"
              >
                Show all companies
              </button>
            ) : undefined
          }
        >
          Only companies with at least one currently-open role appear here.
        </EmptyState>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {companies.map((c) => (
          <CompanyRow
            key={c.id}
            id={c.id}
            name={c.name}
            meta={[
              `${c.open_job_count} open role${c.open_job_count === 1 ? "" : "s"}`,
              c.size_bucket ? (SIZE_LABEL[c.size_bucket] ?? c.size_bucket) : null,
              c.hq_city,
            ]
              .filter(Boolean)
              .join(" · ")}
          />
        ))}
      </div>
    </div>
  );
}

// Same stretched-link pattern as JobCard: the row is an article with a
// full-size link, and the Watch button sits above it — a <button> can't
// live inside an <a>.
function CompanyRow({ id, name, meta, highlighted = false }: { id: string; name: string; meta: string; highlighted?: boolean }) {
  return (
    <article
      className={`relative flex items-center gap-3.5 rounded-[var(--radius-card)] border px-4 sm:px-5 py-4 transition-colors duration-100 ${
        highlighted
          ? "border-accent-primary/30 bg-accent-subtle hover:border-accent-primary/50"
          : "border-border-subtle bg-bg-surface hover:border-border-strong"
      }`}
    >
      <CompanyAvatar id={id} name={name} />
      <div className="min-w-0 flex-1">
        <h3 className="text-label font-semibold text-text-primary truncate">
          <Link href={`/companies/${id}`} className="after:absolute after:inset-0 after:content-[''] after:rounded-[var(--radius-card)]">
            {name}
          </Link>
        </h3>
        <p className="text-caption text-text-muted mt-0.5 truncate">{meta}</p>
      </div>
      <div className="relative z-10 shrink-0">
        <WatchButton companyId={id} size="sm" />
      </div>
    </article>
  );
}

export default function CompaniesPage() {
  return (
    <Suspense>
      <CompaniesContent />
    </Suspense>
  );
}
