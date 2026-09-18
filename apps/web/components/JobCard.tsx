import Link from "next/link";
import type { JobFeedItem } from "@/lib/types";
import { SENIORITY_LABEL } from "@/lib/taxonomy";
import { ScoreBadge } from "./ScoreBadge";
import { StateControls } from "./StateControls";
import { CompanyAvatar } from "./CompanyAvatar";

function timeAgo(iso?: string): string | null {
  if (!iso) return null;
  const diffMs = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diffMs / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function locationLabel(loc: JobFeedItem["location"]): string {
  const parts: string[] = [];
  if (loc.city) parts.push(loc.city);
  else if (loc.country) parts.push(loc.country);
  const mode = loc.work_mode !== "unknown" ? loc.work_mode : null;
  if (parts.length === 0) return mode ? capitalize(mode) : "Location unknown";
  return mode ? `${parts.join(", ")} (${capitalize(mode)})` : parts.join(", ");
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function formatComp(comp: JobFeedItem["compensation"]): string | null {
  if (!comp) return null;
  const amount = Math.round(parseFloat(comp.normalized_inr_month));
  if (Number.isNaN(amount)) return null;
  return `₹${amount.toLocaleString("en-IN")}/mo`;
}

// Days until a deadline, or null when there is none or it has passed.
// Only deadlines within two weeks get a chip — a date three months out is
// noise on a card, and the detail page shows the full date regardless.
export function deadlineChip(iso?: string): { label: string; urgent: boolean } | null {
  if (!iso) return null;
  const days = Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
  if (days < 0 || days > 14) return null;
  if (days === 0) return { label: "Closes today", urgent: true };
  if (days === 1) return { label: "Closes tomorrow", urgent: true };
  return { label: `Closes in ${days}d`, urgent: days <= 3 };
}

// The card is an <article> with a "stretched" title link (the link's
// ::after covers the whole card), not an <a> wrapping everything — a
// <button> inside an <a> is invalid HTML, and screen readers announce the
// whole card as one link with the action buttons lost inside it. The
// action row sits above the stretched link (relative z-10) so its
// buttons stay clickable.
const CARD_LINK =
  "after:absolute after:inset-0 after:content-[''] after:rounded-[var(--radius-card)] focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:outline-accent-primary";

export function JobCard({ job }: { job: JobFeedItem }) {
  // "unknown" renders blank here (card text stays clean when
  // classification found no signal), unlike lib/taxonomy's own
  // "Unspecified" label — that one is for FilterBar, where an option
  // needs a visible name to click on.
  const seniority = job.seniority === "unknown" ? "" : (SENIORITY_LABEL[job.seniority] ?? "");
  const posted = timeAgo(job.first_seen_at ?? job.posted_at);
  const comp = formatComp(job.compensation);
  const deadline = deadlineChip(job.deadline_at);
  const href = `/opportunities/${job.job_group_id}`;

  // Dismissed: collapsed to one thin row with an undo, per
  // docs/12-frontend-ux.md section 4.2's card states.
  if (job.state === "dismissed") {
    return (
      <article className="relative flex items-center justify-between gap-3 min-h-[38px] rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-4 sm:px-[18px] py-1">
        <Link href={href} className={`${CARD_LINK} text-caption text-text-muted truncate hover:text-text-secondary transition-colors duration-100`}>
          {job.title} · {job.company.name} · dismissed
        </Link>
        <div className="relative z-10 shrink-0">
          <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
        </div>
      </article>
    );
  }

  // Applied: collapsed, muted, checkmarked — already in motion, so the
  // full action row and tags are no longer the useful surface. Still
  // carries StateControls so the Undo affordance is reachable here too.
  if (job.state === "applied") {
    return (
      <article className="relative flex items-center justify-between gap-3 sm:gap-4 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-4 sm:px-5 py-3.5 sm:py-4 opacity-[0.66] hover:opacity-90 transition-opacity duration-100">
        <div className="flex items-center gap-3 min-w-0">
          <CompanyAvatar id={job.company.id} name={job.company.name} size={28} />
          <div className="min-w-0">
            <h3 className="text-[14px] font-semibold text-text-secondary truncate">
              <Link href={href} className={CARD_LINK}>
                {job.title}
              </Link>
            </h3>
            {/* suppressHydrationWarning: posted is Date.now()-relative, so
                the server-rendered string and the client's own recompute a
                moment later can legitimately differ. */}
            <p className="text-caption text-text-muted mt-0.5 truncate" suppressHydrationWarning>
              {job.company.name} · applied {posted ?? "recently"}
            </p>
          </div>
        </div>
        <div className="relative z-10 flex items-center gap-3 shrink-0">
          <ScoreBadge score={job.priority} size="sm" />
          <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
        </div>
      </article>
    );
  }

  return (
    <article
      className={`group relative block rounded-[var(--radius-card)] border bg-bg-surface px-4 pt-5 pb-4 sm:px-7 sm:pt-7 sm:pb-6 transition-all duration-100 hover:border-border-strong hover:-translate-y-px ${
        job.state === "saved" ? "border-border-subtle border-l-2 border-l-accent-primary" : "border-border-subtle"
      }`}
    >
      <div className="flex items-start justify-between gap-4 sm:gap-5">
        <div className="flex items-start gap-3 sm:gap-3.5 min-w-0 flex-1">
          <CompanyAvatar id={job.company.id} name={job.company.name} />
          <div className="min-w-0 flex-1 pt-0.5">
            <div className="flex items-center gap-2 min-w-0">
              <h3 className="text-[15px] leading-5 font-semibold text-text-primary truncate min-w-0">
                <Link href={href} className={CARD_LINK}>
                  {job.title}
                </Link>
              </h3>
              {job.state === "saved" && (
                <span className="text-[9px] font-semibold tracking-[0.04em] uppercase text-accent-primary border border-accent-primary/30 bg-accent-subtle rounded shrink-0 px-1.5 py-px">
                  Saved
                </span>
              )}
            </div>
            <p className="text-caption font-medium text-text-secondary mt-1">
              {job.company.name}
              <span className="text-text-muted font-normal">
                {" "}
                · {seniority && `${seniority} · `}
                {locationLabel(job.location)}
              </span>
            </p>
          </div>
        </div>
        <ScoreBadge score={job.priority} />
      </div>

      {(job.skills.length > 0 || deadline) && (
        <div className="flex flex-wrap items-center gap-1.5 mt-4">
          {deadline && (
            <span
              className={`text-[10px] font-medium px-2.5 py-1 rounded-full border ${
                deadline.urgent ? "border-warning/40 bg-warning/10 text-warning" : "border-border-subtle bg-bg-elevated text-text-secondary"
              }`}
            >
              {deadline.label}
            </span>
          )}
          {job.skills.slice(0, 6).map((skill) => (
            <span key={skill} className="text-[10px] font-medium px-2.5 py-1 rounded-full bg-bg-elevated text-text-secondary">
              {skill.replace(/_/g, " ")}
            </span>
          ))}
        </div>
      )}

      <div className="flex items-center justify-between gap-3 mt-5">
        {/* suppressHydrationWarning: posted is Date.now()-relative — see the
            other timeAgo() call site's own comment above. */}
        <span className="text-caption text-text-muted truncate" suppressHydrationWarning>
          {[comp, posted].filter(Boolean).join(" · ")}
        </span>
        <div className="relative z-10 shrink-0">
          <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
        </div>
      </div>
    </article>
  );
}
