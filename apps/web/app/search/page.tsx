"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { searchJobs } from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { Shell } from "@/components/Shell";

export default function SearchPage() {
  const [input, setInput] = useState("");
  const [query, setQuery] = useState("");

  const { data, isLoading, isError, error, isFetched } = useQuery({
    queryKey: ["search", query],
    queryFn: () => searchJobs(query),
    enabled: query.length > 0,
  });

  const results = data?.data ?? [];

  return (
    <Shell>
      <div className="max-w-3xl mx-auto px-6 py-8">
        <h1 className="text-title text-text-primary mb-4">Search</h1>

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
            placeholder="backend intern bengaluru"
            className="flex-1 text-body bg-bg-surface border border-border-subtle rounded-[var(--radius-card)] px-3 py-2 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary"
            autoFocus
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
            Search failed: {error instanceof Error ? error.message : "unknown error"}
          </div>
        )}

        {isLoading && <p className="text-body text-text-muted">Searching…</p>}

        {isFetched && !isLoading && !isError && results.length === 0 && (
          <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
            No matches for &ldquo;{query}&rdquo;.
          </div>
        )}

        {results.length > 0 && (
          <>
            <p className="text-caption text-text-muted mb-3">
              {results.length} result{results.length === 1 ? "" : "s"} for &ldquo;{query}&rdquo; (keyword match)
            </p>
            <div className="space-y-3">
              {results.map((job) => (
                <JobCard key={job.job_group_id} job={job} />
              ))}
            </div>
          </>
        )}
      </div>
    </Shell>
  );
}
