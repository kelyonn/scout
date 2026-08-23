"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import {
  fetchApplications,
  fetchGoogleCalendarEvents,
  fetchGoogleCalendarStatus,
  fetchJobs,
} from "@/lib/api";
import { JobCard } from "@/components/JobCard";
import { NavIcon } from "@/components/NavIcon";
import type { ApplicationItem } from "@/lib/types";

const SENIORITY_FILTER = ["internship", "new_grad", "entry", "unknown"];
const STALE_DAYS = 14;
const WEEK_MS = 7 * 86_400_000;

function daysSince(iso: string): number {
  return Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000);
}

function daysUntil(iso: string): number {
  return Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
}

function isStale(item: ApplicationItem): boolean {
  if (item.state !== "applied" && item.state !== "screening" && item.state !== "interviewing") return false;
  return daysSince(item.state_changed_at) > STALE_DAYS;
}

// Local (not UTC) date key — deadline_at is a date, and bucketing it
// through toISOString would shift it a day for anyone west of Greenwich.
function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function greeting(): string {
  const hour = new Date().getHours();
  if (hour < 12) return "Good morning";
  if (hour < 17) return "Good afternoon";
  return "Good evening";
}

function Tile({ label, value, href }: { label: string; value: number; href: string }) {
  return (
    <Link
      href={href}
      className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-5 py-4 transition-colors duration-100 hover:border-border-strong hover:bg-bg-elevated"
    >
      <div className="text-2xl font-semibold font-mono tabular-nums text-text-primary">{value}</div>
      <div className="text-caption text-text-muted mt-1">{label}</div>
    </Link>
  );
}

const QUICK_LINKS: { label: string; icon: string; href: string }[] = [
  { label: "Opportunities", icon: "opportunities", href: "/opportunities" },
  { label: "Search", icon: "search", href: "/search" },
  { label: "Pipeline", icon: "pipeline", href: "/pipeline" },
  { label: "Companies", icon: "companies", href: "/companies" },
  { label: "Insights", icon: "insights", href: "/insights" },
  { label: "Resume", icon: "resume", href: "/resume" },
  { label: "Settings", icon: "settings", href: "/settings" },
];

function QuickLinks() {
  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
      <h2 className="text-heading text-text-primary mb-3">Jump to</h2>
      <div className="grid grid-cols-2 gap-2">
        {QUICK_LINKS.map((link) => (
          <Link
            key={link.href}
            href={link.href}
            className="flex items-center gap-2 rounded-[var(--radius-input)] px-2.5 py-2 text-caption text-text-secondary transition-colors duration-100 hover:bg-bg-elevated hover:text-text-primary"
          >
            <NavIcon name={link.icon} className="shrink-0 text-text-muted" />
            <span className="truncate">{link.label}</span>
          </Link>
        ))}
      </div>
    </div>
  );
}

function CalendarWidget({ applications }: { applications: ApplicationItem[] }) {
  const { data: googleStatus } = useQuery({
    queryKey: ["google-calendar-status"],
    queryFn: fetchGoogleCalendarStatus,
  });
  const { data: googleEvents } = useQuery({
    queryKey: ["google-calendar-events"],
    queryFn: fetchGoogleCalendarEvents,
    enabled: googleStatus?.connected === true,
  });

  const deadlinesByDay = new Map<string, number>();
  for (const item of applications) {
    if (!item.deadline_at) continue;
    const key = dateKey(new Date(item.deadline_at));
    deadlinesByDay.set(key, (deadlinesByDay.get(key) ?? 0) + 1);
  }

  const today = new Date();
  const week = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(today);
    d.setDate(d.getDate() + i);
    return {
      date: d,
      key: dateKey(d),
      label: d.toLocaleDateString("en-IN", { weekday: "narrow" }),
      isToday: i === 0,
      count: deadlinesByDay.get(dateKey(d)) ?? 0,
    };
  });

  const upcoming = applications
    .filter((a): a is ApplicationItem & { deadline_at: string } => !!a.deadline_at && daysUntil(a.deadline_at) >= 0)
    .sort((a, b) => new Date(a.deadline_at).getTime() - new Date(b.deadline_at).getTime())
    .slice(0, 3);

  const nextGoogleEvents = (googleEvents?.data ?? []).slice(0, 2);

  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-heading text-text-primary">Calendar</h2>
        <Link
          href="/calendar"
          className="text-caption text-text-muted hover:text-text-primary transition-colors duration-100"
        >
          Open →
        </Link>
      </div>

      <div className="grid grid-cols-7 gap-1.5 mb-4">
        {week.map((day) => (
          <div
            key={day.key}
            title={day.date.toLocaleDateString("en-IN", { weekday: "long", month: "short", day: "numeric" })}
            className={`flex flex-col items-center gap-1 rounded-[var(--radius-input)] py-2 ${
              day.isToday ? "bg-accent-subtle border border-accent-primary/30" : "border border-transparent"
            }`}
          >
            <span className={`text-micro ${day.isToday ? "text-accent-primary" : "text-text-faint"}`}>
              {day.label}
            </span>
            <span
              className={`text-caption font-mono tabular-nums ${day.isToday ? "text-accent-primary font-semibold" : "text-text-secondary"}`}
            >
              {day.date.getDate()}
            </span>
            <span
              className={`w-1 h-1 rounded-full ${day.count > 0 ? "bg-warning" : "bg-transparent"}`}
              aria-hidden
            />
          </div>
        ))}
      </div>

      {upcoming.length === 0 && nextGoogleEvents.length === 0 && (
        <p className="text-caption text-text-muted">Nothing on the calendar this week.</p>
      )}

      <div className="space-y-2">
        {upcoming.map((item) => (
          <Link
            key={item.job_group_id}
            href={`/opportunities/${item.job_group_id}`}
            className="flex items-center justify-between gap-2 rounded-[var(--radius-input)] px-2.5 py-1.5 -mx-2.5 transition-colors duration-100 hover:bg-bg-elevated"
          >
            <span className="text-caption text-text-secondary truncate">{item.title}</span>
            <span className="text-caption text-warning shrink-0">
              {daysUntil(item.deadline_at) === 0 ? "Today" : `${daysUntil(item.deadline_at)}d`}
            </span>
          </Link>
        ))}
        {nextGoogleEvents.map((ev, i) => (
          <div
            key={`google-${i}`}
            className="flex items-center justify-between gap-2 rounded-[var(--radius-input)] px-2.5 py-1.5 -mx-2.5"
          >
            <span className="text-caption text-text-muted truncate">{ev.summary}</span>
            <span className="text-caption text-text-faint shrink-0">
              {new Date(ev.date).toLocaleDateString("en-IN", { month: "short", day: "numeric" })}
            </span>
          </div>
        ))}
      </div>

      {googleStatus?.enabled && !googleStatus.connected && (
        <Link
          href="/settings"
          className="block mt-3 text-caption text-center text-text-muted hover:text-text-primary transition-colors duration-100 rounded-[var(--radius-input)] border border-dashed border-border-strong py-2"
        >
          Connect Google Calendar →
        </Link>
      )}
    </div>
  );
}

export default function HomePage() {
  const { data: jobsData, isLoading: jobsLoading } = useQuery({
    queryKey: ["jobs", SENIORITY_FILTER, "home"],
    queryFn: () => fetchJobs({ minPriority: 1, seniority: SENIORITY_FILTER, limit: 6 }),
  });

  const { data: applicationsData, isLoading: applicationsLoading } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  const newJobs = (jobsData?.data ?? []).filter((j) => j.state === "new" || j.state === "viewed");
  const topJobs = newJobs.slice(0, 5);
  const applications = applicationsData?.data ?? [];

  const saved = applications.filter((a) => a.state === "saved").length;
  const applied = applications.filter((a) => a.state === "applied").length;
  const interviewing = applications.filter((a) => a.state === "interviewing").length;
  const stale = applications.filter(isStale);

  const closingBy3 = applications.filter(
    (a) => a.deadline_at && daysUntil(a.deadline_at) >= 0 && daysUntil(a.deadline_at) <= 3,
  ).length;

  const now = new Date().getTime();
  const savedThisWeek = applications.filter(
    (a) => a.state === "saved" && now - new Date(a.state_changed_at).getTime() <= WEEK_MS,
  ).length;
  const appliedThisWeek = applications.filter(
    (a) => a.applied_at && now - new Date(a.applied_at).getTime() <= WEEK_MS,
  ).length;

  // "Needs action" merges deadlines and stale follow-ups into one
  // ranked list — docs/12-frontend-ux.md section 4.1 puts this above
  // new opportunities on purpose: a deadline on something already saved
  // outranks novelty.
  const needsAction: {
    key: string;
    href: string;
    title: string;
    company: string;
    kind: "deadline" | "stale";
    urgency: number;
    right: string;
  }[] = [
    ...applications
      .filter((a) => a.deadline_at && daysUntil(a.deadline_at) >= 0 && daysUntil(a.deadline_at) <= 7)
      .map((a) => ({
        key: `deadline-${a.job_group_id}`,
        href: `/opportunities/${a.job_group_id}`,
        title: a.title,
        company: a.company.name,
        kind: "deadline" as const,
        urgency: daysUntil(a.deadline_at!),
        right: daysUntil(a.deadline_at!) === 0 ? "Closes today" : `Closes in ${daysUntil(a.deadline_at!)}d`,
      })),
    ...stale.map((a) => ({
      key: `stale-${a.job_group_id}`,
      href: `/opportunities/${a.job_group_id}`,
      title: a.title,
      company: a.company.name,
      kind: "stale" as const,
      urgency: 100 - daysSince(a.state_changed_at),
      right: `${daysSince(a.state_changed_at)}d in ${a.state} — follow up?`,
    })),
  ].sort((a, b) => a.urgency - b.urgency);

  const loading = jobsLoading || applicationsLoading;

  return (
    <div className="max-w-6xl mx-auto px-10 py-10">
      <div className="flex items-baseline justify-between mb-8">
        <div>
          <h1 className="text-title text-text-primary">{greeting()}</h1>
          <p className="text-caption text-text-muted mt-1.5">Where things stand right now.</p>
        </div>
        {/* suppressHydrationWarning: this renders the current instant, so
            the server's render and the client's hydration a moment later
            legitimately show different text — same pattern React's own
            docs use a live clock to illustrate. */}
        <span className="text-caption text-text-faint tabular-nums" suppressHydrationWarning>
          {new Date().toLocaleString("en-IN", {
            timeZone: "Asia/Kolkata",
            weekday: "short",
            month: "short",
            day: "numeric",
            hour: "numeric",
            minute: "2-digit",
          })}{" "}
          IST
        </span>
      </div>

      {!loading && (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 mb-10">
          <Tile label="New" value={newJobs.length} href="/opportunities" />
          <Tile label="Closing ≤3d" value={closingBy3} href="/calendar" />
          <Tile label="Interviews" value={interviewing} href="/pipeline" />
          <Tile label="Saved" value={saved} href="/saved" />
          <Tile label="Applied" value={applied} href="/applied" />
          <Tile label="Stale (14d+)" value={stale.length} href="/pipeline" />
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_320px] gap-8">
        <div className="min-w-0">
          {needsAction.length > 0 && (
            <div className="mb-10">
              <h2 className="text-heading text-text-primary mb-3">Needs action</h2>
              <div className="space-y-2">
                {needsAction.map((row) => (
                  <Link
                    key={row.key}
                    href={row.href}
                    className={`flex items-center justify-between rounded-[var(--radius-card)] border border-l-2 border-border-subtle bg-bg-surface px-5 py-3.5 hover:border-border-strong transition-colors duration-100 ${
                      row.kind === "deadline" ? "border-l-warning" : "border-l-border-strong"
                    }`}
                  >
                    <div className="min-w-0">
                      <p className="text-label font-semibold text-text-primary truncate">{row.title}</p>
                      <p className="text-caption text-text-muted mt-0.5">{row.company}</p>
                    </div>
                    <span
                      className={`text-caption shrink-0 ml-4 ${row.kind === "deadline" ? "text-warning" : "text-text-muted"}`}
                    >
                      {row.right}
                    </span>
                  </Link>
                ))}
              </div>
            </div>
          )}

          <div className="mb-10">
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-heading text-text-primary">New since you last looked</h2>
              <Link
                href="/opportunities"
                className="text-caption text-text-muted hover:text-text-primary transition-colors duration-100"
              >
                See all {newJobs.length} →
              </Link>
            </div>
            {jobsLoading && <p className="text-body text-text-muted">Loading…</p>}
            {!jobsLoading && topJobs.length === 0 && (
              <div className="text-body text-text-secondary rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
                Nothing new right now — check Opportunities for the full ranked feed.
              </div>
            )}
            <div className="space-y-4">
              {topJobs.map((job) => (
                <JobCard key={job.job_group_id} job={job} />
              ))}
            </div>
          </div>

          {!loading && (
            <p className="text-caption text-text-muted">
              This week: <span className="text-text-secondary tabular-nums">{savedThisWeek}</span> saved ·{" "}
              <span className="text-text-secondary tabular-nums">{appliedThisWeek}</span> applied ·{" "}
              <span className="text-text-secondary tabular-nums">{interviewing}</span> interviewing
            </p>
          )}
        </div>

        <div className="space-y-6">
          <CalendarWidget applications={applications} />
          <QuickLinks />
        </div>
      </div>
    </div>
  );
}
