import Link from "next/link";
import type { JobFeedItem } from "@/lib/types";
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

const SENIORITY_LABEL: Record<string, string> = {
  internship: "Internship",
  new_grad: "New grad",
  entry: "Entry",
  mid: "Mid",
  senior: "Senior",
  staff: "Staff",
  unknown: "",
};

export function JobCard({ job }: { job: JobFeedItem }) {
  const seniority = SENIORITY_LABEL[job.seniority] ?? "";
  const posted = timeAgo(job.first_seen_at ?? job.posted_at);
  const comp = formatComp(job.compensation);

  // Dismissed: collapsed to one thin row with an undo, per
  // docs/12-frontend-ux.md section 4.2's card states.
  if (job.state === "dismissed") {
    return (
      <div className="flex items-center justify-between gap-3 h-[34px] rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-[18px]">
        <Link
          href={`/opportunities/${job.job_group_id}`}
          className="text-caption text-text-muted truncate hover:text-text-secondary transition-colors duration-100"
        >
          {job.title} · {job.company.name} · dismissed
        </Link>
        <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
      </div>
    );
  }

  // Applied: collapsed, muted, checkmarked — already in motion, so the
  // full action row and tags are no longer the useful surface. Still
  // carries StateControls (not just a Link) so the Undo affordance is
  // reachable here too — the point where a stray "Apply now" click needs
  // fixing is exactly this collapsed view, not just the detail page.
  if (job.state === "applied") {
    return (
      <div className="flex items-center justify-between gap-4 rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-5 py-4 opacity-[0.66] hover:opacity-90 transition-opacity duration-100">
        <Link href={`/opportunities/${job.job_group_id}`} className="flex items-center gap-3 min-w-0">
          <CompanyAvatar id={job.company.id} name={job.company.name} size={28} />
          <div className="min-w-0">
            <h3 className="text-[14px] font-semibold text-text-secondary truncate">{job.title}</h3>
            {/* suppressHydrationWarning: posted is Date.now()-relative, so
                the server-rendered string and the client's own recompute a
                moment later can legitimately differ (e.g. straddling a
                minute boundary) without either one being wrong. */}
            <p className="text-caption text-text-muted mt-0.5" suppressHydrationWarning>
              {job.company.name} · applied {posted ?? "recently"}
            </p>
          </div>
        </Link>
        <div className="flex items-center gap-3 shrink-0">
          <ScoreBadge score={job.priority} size="sm" />
          <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
        </div>
      </div>
    );
  }

  return (
    <Link
      href={`/opportunities/${job.job_group_id}`}
      className="group block rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-7 pt-7 pb-6 transition-all duration-100 hover:border-border-strong hover:-translate-y-px"
    >
      <div className="flex items-start justify-between gap-5">
        <div className="flex items-start gap-3.5 min-w-0 flex-1">
          <CompanyAvatar id={job.company.id} name={job.company.name} />
          <div className="min-w-0 flex-1 pt-0.5">
            <div className="flex items-center gap-2 min-w-0">
              <h3 className="text-[15px] leading-5 font-semibold text-text-primary truncate min-w-0">
                {job.title}
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

      {job.skills.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mt-4">
          {job.skills.slice(0, 6).map((skill) => (
            <span
              key={skill}
              className="text-[10px] font-medium px-2.5 py-1 rounded-full bg-bg-elevated text-text-secondary"
            >
              {skill.replace(/_/g, " ")}
            </span>
          ))}
        </div>
      )}

      <div className="flex items-center justify-between mt-5">
        {/* suppressHydrationWarning: posted is Date.now()-relative — see the
            other timeAgo() call site's own comment above. */}
        <span className="text-caption text-text-muted" suppressHydrationWarning>
          {[comp, posted].filter(Boolean).join(" · ")}
        </span>
        <StateControls groupId={job.job_group_id} state={job.state} applyUrl={job.apply_url} size="sm" />
      </div>
    </Link>
  );
}
