"use client";

import { use, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { ApiError, errorMessage, fetchJobDetail, updateJobState } from "@/lib/api";
import { ScoreBadge } from "@/components/ScoreBadge";
import { ScoreRow } from "@/components/ScoreRow";
import { DescriptionHtml } from "@/components/DescriptionHtml";
import { StateControls } from "@/components/StateControls";
import { useToast } from "@/components/Toast";
import { EmptyState, ErrorNotice, Skeleton } from "@/components/ui";
import { STATE_LABEL } from "@/lib/stateMachine";
import { SENIORITY_LABEL, WORK_MODE_LABEL } from "@/lib/taxonomy";
import type { JobDetail } from "@/lib/types";

const MAX_NOTES = 4000;

export default function JobDetailPage({ params }: { params: Promise<{ groupId: string }> }) {
  const { groupId } = use(params);
  const { data: job, isPending, isError, error, refetch } = useQuery({
    queryKey: ["job-detail", groupId],
    queryFn: () => fetchJobDetail(groupId),
    // The AI summary generates on demand (apps/api/internal/jobs's
    // Detail handler enqueues it on first view, per summary_pending in
    // the response) — a local LLM call typically finishes in a few
    // seconds, so a short poll while pending is simpler and more honest
    // about the wait than a spinner. Stops once summary_pending flips
    // false, or after ~2 minutes so a stalled brain worker doesn't poll
    // forever.
    refetchInterval: (query) => {
      if (!query.state.data?.summary_pending) return false;
      return query.state.dataUpdateCount > 48 ? false : 2500;
    },
  });

  const notFound = isError && error instanceof ApiError && error.isNotFound;

  return (
    <div className="max-w-5xl mx-auto page">
      <Link href="/opportunities" className="text-label text-text-muted hover:text-text-secondary transition-colors duration-100">
        ← Opportunities
      </Link>

      {isPending && (
        <div className="mt-6 grid grid-cols-1 lg:grid-cols-[1fr_320px] gap-8" role="status" aria-label="Loading">
          <div className="space-y-4">
            <Skeleton className="h-8 w-2/3" />
            <Skeleton className="h-5 w-1/3" />
            <Skeleton className="h-24" />
            <Skeleton className="h-64" />
          </div>
          <div className="space-y-4">
            <Skeleton className="h-14" />
            <Skeleton className="h-80" />
          </div>
        </div>
      )}

      {notFound && (
        <div className="mt-6">
          <EmptyState title="This job isn't available anymore" action={{ label: "Back to Opportunities", href: "/opportunities" }}>
            It was removed from its source or has closed. Anything you saved about it is still in your Pipeline.
          </EmptyState>
        </div>
      )}

      {isError && !notFound && (
        <div className="mt-6">
          <ErrorNotice error={error} fallback="Couldn't load this job." onRetry={() => void refetch()} />
        </div>
      )}

      {job && <JobDetailView job={job} />}
    </div>
  );
}

function JobDetailView({ job }: { job: JobDetail }) {
  const meta = [
    formatCompensation(job.compensation),
    job.posted_at ? `Posted ${formatDate(job.posted_at)}` : null,
    job.deadline_at ? `Closes ${formatDate(job.deadline_at)}` : null,
  ].filter((s): s is string => !!s);

  const listingUrl = job.apply_url || job.canonical_url;

  return (
    <div className="mt-6 grid grid-cols-1 lg:grid-cols-[1fr_320px] gap-8">
      {/* Left column — scrollable content */}
      <div className="min-w-0">
        <h1 className="text-title text-text-primary">{job.title}</h1>
        <p className="text-body text-text-secondary mt-1">
          <Link href={`/companies/${job.company.id}`} className="hover:text-text-primary transition-colors duration-100">
            {job.company.name}
          </Link>
          {job.location.city && ` · ${job.location.city}`}
          {!job.location.city && job.location.country && ` · ${job.location.country}`}
          {job.location.work_mode !== "unknown" && ` · ${WORK_MODE_LABEL[job.location.work_mode] ?? job.location.work_mode}`}
          {job.seniority !== "unknown" && ` · ${SENIORITY_LABEL[job.seniority] ?? job.seniority}`}
        </p>
        {meta.length > 0 && (
          <p className="text-caption text-text-muted mt-1.5 font-mono tabular-nums">{meta.join(" · ")}</p>
        )}

        {job.skills.length > 0 && (
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
        )}

        <div className="mt-6 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
          <p className="text-micro text-text-muted mb-2">Summary</p>
          {job.ai_summary ? (
            <p className="text-body text-text-secondary">{job.ai_summary}</p>
          ) : (
            <p className="text-body text-text-muted italic" aria-live="polite">
              {job.summary_pending ? "Generating summary…" : "No summary available for this posting."}
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
              <p className="text-body text-text-secondary whitespace-pre-wrap">{job.description_text}</p>
            )}
          </Section>
        )}

        {job.company.description && (
          <Section title={`About ${job.company.name}`}>
            <p className="text-body text-text-secondary">{job.company.description}</p>
          </Section>
        )}

        {/* Keyed on the saved notes so a change persisted elsewhere (another
            tab, the pipeline) remounts the editor with the new text rather
            than needing a setState-in-effect to reconcile the draft. */}
        <NotesSection key={job.notes ?? ""} groupId={job.job_group_id} state={job.state} initialNotes={job.notes ?? ""} />

        <Section title={`Sources (${job.source_count})`}>
          <p className="text-caption text-text-muted">
            Seen {job.source_count} time{job.source_count === 1 ? "" : "s"} across postings merged into this listing, first on{" "}
            {formatDate(job.first_seen_at)}.
          </p>
        </Section>
      </div>

      {/* Right column — sticky score panel */}
      <div className="lg:sticky lg:top-8 lg:self-start space-y-4">
        <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
          <div className="flex items-center justify-between mb-3">
            <span className="text-label text-text-secondary">Status</span>
            <span className="text-label text-text-primary">{STATE_LABEL[job.state]}</span>
          </div>
          <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} />
          {job.found_elsewhere_first && (
            <p className="text-caption text-text-muted mt-2">You found this one elsewhere before Scout.</p>
          )}
          {listingUrl && (
            <a
              href={listingUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-3 block text-center text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle py-2 transition-colors duration-100 hover:border-border-strong hover:text-text-primary"
            >
              View original listing ↗
            </a>
          )}
        </div>

        {job.scores ? (
          <>
            <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
              <div className="flex items-center justify-between">
                <span className="text-label text-text-secondary">Priority</span>
                <ScoreBadge score={job.scores.priority} />
              </div>
            </div>

            <details open className="group rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface">
              <summary className="cursor-pointer list-none px-4 py-3 text-label text-text-secondary flex items-center justify-between">
                Score breakdown
                <span aria-hidden className="text-text-faint transition-transform duration-150 group-open:rotate-90">
                  ›
                </span>
              </summary>
              <div className="px-4 pb-4 space-y-2.5">
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
            </details>
          </>
        ) : (
          <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 text-caption text-text-muted">
            Not scored yet — scoring runs shortly after a posting is collected.
          </div>
        )}
      </div>
    </div>
  );
}

// Free-text notes on a job — interview prep, a recruiter's name, why it
// was dismissed. Stored on the same user_job_state row the state
// machine writes (POST /state with the current state and a notes field
// is a no-op transition that keeps state and updates notes).
function NotesSection({ groupId, state, initialNotes }: { groupId: string; state: JobDetail["state"]; initialNotes: string }) {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [notes, setNotes] = useState(initialNotes);

  const save = useMutation({
    mutationFn: () => updateJobState(groupId, { state, notes: notes.trim() }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["job-detail", groupId] });
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      toast("Notes saved", { kind: "success" });
    },
    onError: (err) => toast(errorMessage(err, "Couldn't save notes."), { kind: "error" }),
  });

  const dirty = notes.trim() !== initialNotes.trim();
  const tooLong = notes.length > MAX_NOTES;

  return (
    <Section title="Your notes">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!dirty || tooLong || save.isPending) return;
          save.mutate();
        }}
      >
        <label htmlFor="job-notes" className="sr-only">
          Notes about this job
        </label>
        <textarea
          id="job-notes"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          rows={4}
          placeholder="Recruiter contact, questions to ask, why you're on the fence…"
          aria-invalid={tooLong || undefined}
          aria-describedby="job-notes-hint"
          className="w-full text-body bg-bg-surface border border-border-subtle rounded-[var(--radius-card)] px-3 py-2.5 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary resize-y"
        />
        <div className="mt-2 flex items-center justify-between gap-3">
          <span id="job-notes-hint" className={`text-caption ${tooLong ? "text-danger" : "text-text-faint"}`}>
            {tooLong ? `Notes are limited to ${MAX_NOTES.toLocaleString()} characters.` : "Private to you. Saved with this job."}
          </span>
          <button
            type="submit"
            disabled={!dirty || tooLong || save.isPending}
            className="text-label text-white bg-accent-primary rounded-[var(--radius-input)] px-3 py-1.5 transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {save.isPending ? "Saving…" : "Save notes"}
          </button>
        </div>
      </form>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-8">
      <h2 className="text-heading text-text-primary mb-2">{title}</h2>
      {children}
    </section>
  );
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" });
}

function formatCompensation(comp: JobDetail["compensation"]): string | null {
  if (!comp) return null;
  if (comp.normalized_inr_month) {
    const amount = Math.round(parseFloat(comp.normalized_inr_month));
    if (!Number.isNaN(amount)) return `₹${amount.toLocaleString("en-IN")}/mo`;
  }
  if (comp.min || comp.max) {
    const range = [comp.min, comp.max].filter(Boolean).join("–");
    return comp.currency ? `${comp.currency} ${range}` : range;
  }
  if (comp.paid === "unpaid") return "Unpaid";
  return null;
}
