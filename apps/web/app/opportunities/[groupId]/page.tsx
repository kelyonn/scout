"use client";

import { use } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchJobDetail } from "@/lib/api";
import { Shell } from "@/components/Shell";
import { ScoreBadge } from "@/components/ScoreBadge";
import { ScoreRow } from "@/components/ScoreRow";
import { DescriptionHtml } from "@/components/DescriptionHtml";
import { StateControls } from "@/components/StateControls";
import { STATE_LABEL } from "@/lib/stateMachine";

export default function JobDetailPage({
  params,
}: {
  params: Promise<{ groupId: string }>;
}) {
  const { groupId } = use(params);
  const { data: job, isLoading, isError, error } = useQuery({
    queryKey: ["job-detail", groupId],
    queryFn: () => fetchJobDetail(groupId),
    // The AI summary generates on demand (apps/api/internal/jobs's
    // Detail handler enqueues it on first view, per
    // summary_pending in the response) — a local LLM call typically
    // finishes in a few seconds, so a short poll while pending is
    // simpler and more honest about the wait than a spinner with no
    // sense of progress. Stops the moment summary_pending flips false.
    refetchInterval: (query) => (query.state.data?.summary_pending ? 2500 : false),
  });

  return (
    <Shell>
      <div className="max-w-5xl mx-auto px-6 py-8">
        <Link
          href="/opportunities"
          className="text-label text-text-muted hover:text-text-secondary transition-colors duration-100"
        >
          ← Opportunities
        </Link>

        {isLoading && <p className="text-body text-text-muted mt-6">Loading…</p>}
        {isError && (
          <div className="mt-6 text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
            {error instanceof Error ? error.message : "Failed to load job"}
          </div>
        )}

        {job && (
          <div className="mt-6 grid grid-cols-1 lg:grid-cols-[1fr_320px] gap-8">
            {/* Left column — scrollable content */}
            <div className="min-w-0">
              <h1 className="text-title text-text-primary">{job.title}</h1>
              <p className="text-body text-text-secondary mt-1">
                {job.company.name}
                {job.location.city && ` · ${job.location.city}`}
                {job.location.work_mode !== "unknown" &&
                  ` · ${capitalize(job.location.work_mode)}`}
              </p>

              <div className="flex flex-wrap gap-1.5 mt-3">
                {job.skills.map((skill) => (
                  <span
                    key={skill}
                    className="text-micro px-1.5 py-0.5 rounded-[var(--radius-input)] bg-bg-elevated text-text-muted border border-border-subtle"
                  >
                    {skill.replace(/_/g, " ")}
                  </span>
                ))}
              </div>

              <div className="mt-6 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
                <p className="text-micro text-text-muted mb-2">Summary</p>
                {job.ai_summary ? (
                  <p className="text-body text-text-secondary">{job.ai_summary}</p>
                ) : (
                  <p className="text-body text-text-muted italic">
                    {job.summary_pending ? "Generating summary…" : "Summary unavailable."}
                  </p>
                )}
              </div>

              {job.scores?.explanation && (
                <Section title="Why this matches">
                  <p className="text-body text-text-secondary">{job.scores.explanation}</p>
                </Section>
              )}

              {(job.description_html || job.description_text) && (
                <Section title="Description">
                  {job.description_html ? (
                    <DescriptionHtml html={job.description_html} />
                  ) : (
                    <p className="text-body text-text-secondary whitespace-pre-wrap">
                      {job.description_text}
                    </p>
                  )}
                </Section>
              )}

              {job.company.description && (
                <Section title={`About ${job.company.name}`}>
                  <p className="text-body text-text-secondary">{job.company.description}</p>
                </Section>
              )}

              <Section title={`Sources (${job.source_count})`}>
                <p className="text-caption text-text-muted">
                  Seen {job.source_count} time{job.source_count === 1 ? "" : "s"} across
                  postings merged into this listing, first {new Date(job.first_seen_at).toLocaleDateString()}.
                </p>
              </Section>
            </div>

            {/* Right column — sticky score panel */}
            <div className="lg:sticky lg:top-8 lg:self-start space-y-4">
              {job.scores && (
                <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-label text-text-secondary">Priority</span>
                    <ScoreBadge score={job.scores.priority} />
                  </div>
                </div>
              )}

              {job.scores && (
                <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 space-y-2.5">
                  <ScoreRow label="Overall match" value={job.scores.overall_match} />
                  <ScoreRow label="Skill match" value={job.scores.skill_match} />
                  <ScoreRow label="Resume match" value={job.scores.resume_match} />
                  <ScoreRow label="Company quality" value={job.scores.company_quality} />
                  <ScoreRow label="Compensation" value={job.scores.compensation} />
                  <ScoreRow label="Learning" value={job.scores.learning_opportunity} />
                  <ScoreRow label="Culture" value={job.scores.engineering_culture} />
                  <ScoreRow label="Growth" value={job.scores.growth_potential} />
                  <ScoreRow label="Interview prob." value={job.scores.interview_probability} />
                  <ScoreRow label="Competition" value={job.scores.competition_estimate} />
                  <ScoreRow label="Ease of apply" value={job.scores.ease_of_applying} />
                  <ScoreRow label="Urgency" value={job.scores.deadline_urgency} />
                  <div className="pt-2 mt-1 border-t border-border-subtle flex items-center justify-between text-caption text-text-muted font-mono tabular-nums">
                    <span>Location ×{job.scores.location_multiplier.toFixed(2)}</span>
                    <span>Fresh ×{job.scores.freshness_multiplier.toFixed(2)}</span>
                  </div>
                </div>
              )}

              <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
                <div className="flex items-center justify-between mb-3">
                  <span className="text-label text-text-secondary">Status</span>
                  <span className="text-label text-text-primary">{STATE_LABEL[job.state]}</span>
                </div>
                <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} />
                {job.found_elsewhere_first && (
                  <p className="text-caption text-text-muted mt-2">
                    You found this one elsewhere before Scout.
                  </p>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mt-8">
      <h2 className="text-heading text-text-primary mb-2">{title}</h2>
      {children}
    </div>
  );
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
