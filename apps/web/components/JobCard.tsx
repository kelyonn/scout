import Link from "next/link";
import type { JobFeedItem } from "@/lib/types";
import { ScoreBadge } from "./ScoreBadge";
import { StateControls } from "./StateControls";

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

const SENIORITY_LABEL: Record<string, string> = {
  internship: "Internship",
  new_grad: "New grad",
  entry: "Entry",
  mid: "Mid",
  senior: "Senior",
  staff: "Staff",
  unknown: "",
};

// docs/12-frontend-ux.md section 4.2's card states: saved gets an accent
// left border, applied is muted, dismissed collapses to one line (with an
// undo, via StateControls itself rather than this border treatment).
function stateBorderClass(state: JobFeedItem["state"]): string {
  if (state === "saved") return "border-l-2 border-l-accent-primary";
  if (state === "applied") return "opacity-70";
  return "";
}

export function JobCard({ job }: { job: JobFeedItem }) {
  const seniority = SENIORITY_LABEL[job.seniority] ?? "";
  const posted = timeAgo(job.first_seen_at ?? job.posted_at);
  const comp = formatComp(job.compensation);

  if (job.state === "dismissed") {
    return (
      <div className="flex items-center justify-between gap-3 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-4 py-2">
        <Link
          href={`/opportunities/${job.job_group_id}`}
          className="text-label text-text-muted truncate hover:text-text-secondary transition-colors duration-100"
        >
          {job.title} · {job.company.name}
        </Link>
        <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
      </div>
    );
  }

  return (
    <Link
      href={`/opportunities/${job.job_group_id}`}
      className={`group block rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 transition-all duration-100 hover:border-border-strong hover:-translate-y-px ${stateBorderClass(job.state)}`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-heading text-text-primary truncate">{job.title}</h3>
          <p className="text-label text-text-secondary mt-0.5">
            {job.company.name}
            {seniority && <span className="text-text-muted"> · {seniority}</span>}
          </p>
        </div>
        <ScoreBadge score={job.priority} size="sm" />
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-3 text-caption text-text-secondary">
        <span>{locationLabel(job.location)}</span>
        {comp && <span className="font-mono tabular-nums">{comp}</span>}
        {posted && <span>{posted}</span>}
      </div>

      {job.skills.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mt-3">
          {job.skills.slice(0, 6).map((skill) => (
            <span
              key={skill}
              className="text-micro px-1.5 py-0.5 rounded-[var(--radius-input)] bg-bg-elevated text-text-muted border border-border-subtle"
            >
              {skill.replace(/_/g, " ")}
            </span>
          ))}
        </div>
      )}

      <div className="mt-3 pt-3 border-t border-border-subtle">
        <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
      </div>
    </Link>
  );
}
