"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchCompanies, fetchWatchlist } from "@/lib/api";
import { CompanyAvatar } from "@/components/CompanyAvatar";
import { WatchButton } from "@/components/WatchButton";

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

export default function CompaniesPage() {
  const [input, setInput] = useState("");
  const [query, setQuery] = useState("");

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["companies", query],
    queryFn: () => fetchCompanies(query || undefined),
  });

  const { data: watchlistData } = useQuery({ queryKey: ["watchlist"], queryFn: fetchWatchlist });

  const companies = data?.data ?? [];
  const watchlist = watchlistData?.data ?? [];

  return (
    <div className="max-w-4xl mx-auto px-10 py-10">
      <div className="mb-8">
        <h1 className="text-title text-text-primary">Companies</h1>
        <p className="text-caption text-text-muted mt-1.5">
          {isLoading ? "Loading…" : `${companies.length} hiring right now`}
        </p>
      </div>

      {watchlist.length > 0 && (
        <div className="mb-8">
          <h2 className="text-heading text-text-primary mb-3">Your watchlist</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {watchlist.map((w) => (
              <Link
                key={w.company_id}
                href={`/companies/${w.company_id}`}
                className="flex items-center gap-3.5 rounded-[var(--radius-card)] border border-accent-primary/30 bg-accent-subtle px-5 py-4 hover:border-accent-primary/50 transition-colors duration-100"
              >
                <CompanyAvatar id={w.company_id} name={w.company_name} />
                <div className="min-w-0 flex-1">
                  <p className="text-label font-semibold text-text-primary truncate">{w.company_name}</p>
                  <p className="text-caption text-text-muted mt-0.5">
                    {w.open_job_count} open role{w.open_job_count === 1 ? "" : "s"}
                  </p>
                </div>
                <WatchButton companyId={w.company_id} size="sm" />
              </Link>
            ))}
          </div>
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(input.trim());
        }}
        className="flex gap-2 mb-6"
      >
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Search companies"
          className="flex-1 text-body bg-bg-surface border border-border-subtle rounded-[var(--radius-card)] px-3 py-2 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary"
        />
        <button
          type="submit"
          className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2 transition-colors duration-100 hover:bg-accent-hover"
        >
          Search
        </button>
      </form>

      {isError && (
        <div className="text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
          Failed to load: {error instanceof Error ? error.message : "unknown error"}
        </div>
      )}

      {!isLoading && !isError && companies.length === 0 && (
        <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
          No companies match right now. Only companies with at least one
          currently-open role show up here.
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {companies.map((c) => (
          <Link
            key={c.id}
            href={`/companies/${c.id}`}
            className="flex items-center gap-3.5 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-5 py-4 hover:border-border-strong transition-colors duration-100"
          >
            <CompanyAvatar id={c.id} name={c.name} />
            <div className="min-w-0 flex-1">
              <p className="text-label font-semibold text-text-primary truncate">{c.name}</p>
              <p className="text-caption text-text-muted mt-0.5">
                {c.open_job_count} open role{c.open_job_count === 1 ? "" : "s"}
                {c.size_bucket && ` · ${SIZE_LABEL[c.size_bucket] ?? c.size_bucket}`}
                {c.hq_city && ` · ${c.hq_city}`}
              </p>
            </div>
            <WatchButton companyId={c.id} size="sm" />
          </Link>
        ))}
      </div>
    </div>
  );
}
