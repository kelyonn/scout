"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { fetchApplications, fetchGoogleCalendarEvents, fetchGoogleCalendarStatus } from "@/lib/api";
import { EmptyState, ErrorNotice, PageHeader, Skeleton } from "@/components/ui";
import type { ApplicationItem, GoogleCalendarEvent } from "@/lib/types";

function dayLabel(iso: string): string {
  const days = Math.round(
    (new Date(iso).setHours(0, 0, 0, 0) - new Date().setHours(0, 0, 0, 0)) / 86_400_000,
  );
  if (days === 0) return "Today";
  if (days === 1) return "Tomorrow";
  if (days < 0) return "Overdue";
  if (days <= 7) return `In ${days} days`;
  return new Date(iso).toLocaleDateString("en-IN", { month: "short", day: "numeric" });
}

function groupKey(iso: string): string {
  const days = Math.round(
    (new Date(iso).setHours(0, 0, 0, 0) - new Date().setHours(0, 0, 0, 0)) / 86_400_000,
  );
  if (days < 0) return "Overdue";
  if (days === 0) return "Today";
  if (days <= 7) return "This week";
  if (days <= 30) return "This month";
  return "Later";
}

const GROUP_ORDER = ["Overdue", "Today", "This week", "This month", "Later"];
const WEEKDAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// Local (not UTC) date key — deadline_at and Google event dates are
// calendar dates, and bucketing them through toISOString would shift
// anyone west of Greenwich onto the wrong day.
function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function ChevronIcon({ direction }: { direction: "left" | "right" }) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path
        d={direction === "left" ? "M10.5 3L5.5 8L10.5 13" : "M5.5 3L10.5 8L5.5 13"}
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

type DayCell = {
  date: Date;
  key: string;
  inMonth: boolean;
  isToday: boolean;
  deadlines: (ApplicationItem & { deadline_at: string })[];
  events: GoogleCalendarEvent[];
};

function MonthGrid({
  items,
  events,
}: {
  items: (ApplicationItem & { deadline_at: string })[];
  events: GoogleCalendarEvent[];
}) {
  const [cursor, setCursor] = useState(() => {
    const d = new Date();
    d.setDate(1);
    d.setHours(0, 0, 0, 0);
    return d;
  });

  const todayKey = dateKey(new Date());

  const deadlinesByDay = useMemo(() => {
    const map = new Map<string, (ApplicationItem & { deadline_at: string })[]>();
    for (const item of items) {
      const key = dateKey(new Date(item.deadline_at));
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(item);
    }
    return map;
  }, [items]);

  const eventsByDay = useMemo(() => {
    const map = new Map<string, GoogleCalendarEvent[]>();
    for (const ev of events) {
      const key = dateKey(new Date(ev.date));
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(ev);
    }
    return map;
  }, [events]);

  const weeks = useMemo(() => {
    const gridStart = new Date(cursor);
    gridStart.setDate(gridStart.getDate() - gridStart.getDay());

    const cells: DayCell[] = [];
    for (let i = 0; i < 42; i++) {
      const date = new Date(gridStart);
      date.setDate(date.getDate() + i);
      const key = dateKey(date);
      cells.push({
        date,
        key,
        inMonth: date.getMonth() === cursor.getMonth(),
        isToday: key === todayKey,
        deadlines: deadlinesByDay.get(key) ?? [],
        events: eventsByDay.get(key) ?? [],
      });
    }

    const out: DayCell[][] = [];
    for (let i = 0; i < cells.length; i += 7) out.push(cells.slice(i, i + 7));
    return out;
  }, [cursor, deadlinesByDay, eventsByDay, todayKey]);

  function shiftMonth(delta: number) {
    setCursor((prev) => {
      const next = new Date(prev);
      next.setMonth(next.getMonth() + delta);
      return next;
    });
  }

  function goToday() {
    const d = new Date();
    d.setDate(1);
    d.setHours(0, 0, 0, 0);
    setCursor(d);
  }

  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-3 sm:p-5 mb-8">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-heading text-text-primary">
          {cursor.toLocaleDateString("en-IN", { month: "long", year: "numeric" })}
        </h2>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={goToday}
            className="text-caption text-text-muted px-2.5 py-1 rounded-[var(--radius-input)] transition-colors duration-100 hover:text-text-primary hover:bg-bg-elevated"
          >
            Today
          </button>
          <button
            type="button"
            onClick={() => shiftMonth(-1)}
            aria-label="Previous month"
            className="w-7 h-7 flex items-center justify-center rounded-[var(--radius-input)] text-text-muted transition-colors duration-100 hover:text-text-primary hover:bg-bg-elevated"
          >
            <ChevronIcon direction="left" />
          </button>
          <button
            type="button"
            onClick={() => shiftMonth(1)}
            aria-label="Next month"
            className="w-7 h-7 flex items-center justify-center rounded-[var(--radius-input)] text-text-muted transition-colors duration-100 hover:text-text-primary hover:bg-bg-elevated"
          >
            <ChevronIcon direction="right" />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-7 gap-1 mb-1">
        {WEEKDAY_LABELS.map((label) => (
          <div key={label} className="text-micro text-text-faint text-center py-1">
            {label}
          </div>
        ))}
      </div>

      <div className="grid grid-cols-7 gap-1">
        {weeks.flat().map((cell) => {
          const total = cell.deadlines.length + cell.events.length;
          const shown = [
            ...cell.deadlines.map((d) => ({
              key: d.job_group_id,
              label: d.title,
              href: `/opportunities/${d.job_group_id}`,
              overdue: groupKey(d.deadline_at) === "Overdue",
              today: groupKey(d.deadline_at) === "Today",
            })),
            ...cell.events.map((e, i) => ({
              key: `ev-${cell.key}-${i}`,
              label: e.summary,
              href: undefined,
              overdue: false,
              today: false,
            })),
          ].slice(0, 2);
          const overflow = total - shown.length;

          return (
            <div
              key={cell.key}
              className={`min-h-[56px] sm:min-h-[92px] rounded-[var(--radius-input)] border p-1 sm:p-1.5 flex flex-col gap-1 ${
                cell.isToday
                  ? "border-accent-primary/40 bg-accent-subtle"
                  : "border-border-subtle bg-bg-base"
              } ${cell.inMonth ? "" : "opacity-40"}`}
            >
              <span
                className={`text-caption font-mono tabular-nums ${
                  cell.isToday ? "text-accent-primary font-semibold" : "text-text-secondary"
                }`}
              >
                {cell.date.getDate()}
              </span>
              <div className="flex flex-col gap-0.5 min-w-0">
                {shown.map((entry) =>
                  entry.href ? (
                    <Link
                      key={entry.key}
                      href={entry.href}
                      title={entry.label}
                      className={`text-micro normal-case tracking-normal truncate rounded px-1 py-0.5 transition-colors duration-100 ${
                        entry.overdue
                          ? "bg-danger/15 text-danger hover:bg-danger/25"
                          : entry.today
                            ? "bg-warning/15 text-warning hover:bg-warning/25"
                            : "bg-accent-subtle text-accent-primary hover:bg-accent-primary/25"
                      }`}
                    >
                      {entry.label}
                    </Link>
                  ) : (
                    <span
                      key={entry.key}
                      title={entry.label}
                      className="text-micro normal-case tracking-normal truncate rounded px-1 py-0.5 bg-bg-elevated text-text-muted"
                    >
                      {entry.label}
                    </span>
                  ),
                )}
                {overflow > 0 && <span className="text-micro normal-case text-text-faint px-1">+{overflow} more</span>}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// Deadlines for jobs already in the pipeline (saved and beyond) — the
// same fetchApplications() call Pipeline and Home both use, so a
// deadline shown here matches what Home's "Deadlines this week" widget
// already promised, rather than pulling from the separately-filtered
// jobs feed and silently disagreeing with it. A real "calendar" (events
// you create, interview scheduling) would need a new table; this is the
// honest subset that's free with data already tracked.
export default function CalendarPage() {
  const { data, isPending, isSuccess, isError, error, refetch } = useQuery({
    queryKey: ["applications"],
    queryFn: () => fetchApplications(),
  });

  const { data: googleStatus } = useQuery({
    queryKey: ["google-calendar-status"],
    queryFn: fetchGoogleCalendarStatus,
  });

  // Only fetched once actually connected — calling this endpoint
  // unconnected is a guaranteed 412 (apps/api/internal/googlecalendar's
  // Events handler), not a real error worth a failed-query state on a
  // page most visitors haven't connected anything on yet.
  const { data: googleEvents } = useQuery({
    queryKey: ["google-calendar-events"],
    queryFn: fetchGoogleCalendarEvents,
    enabled: googleStatus?.connected === true,
  });

  const items = (data?.data ?? []).filter(
    (a): a is ApplicationItem & { deadline_at: string } => !!a.deadline_at,
  );
  items.sort((a, b) => new Date(a.deadline_at).getTime() - new Date(b.deadline_at).getTime());

  const groups = new Map<string, typeof items>();
  for (const item of items) {
    const key = groupKey(item.deadline_at);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(item);
  }

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Calendar"
        subtitle={isPending ? "Loading…" : isError ? undefined : `${items.length} deadline${items.length === 1 ? "" : "s"} across your saved and applied jobs`}
      />

      {isError && (
        <div className="mb-6">
          <ErrorNotice error={error} fallback="Couldn't load deadlines." onRetry={() => void refetch()} />
        </div>
      )}

      {googleStatus?.enabled && !googleStatus.connected && (
        <a
          href="/api/integrations/google/connect"
          className="block mb-8 text-center text-label text-text-secondary rounded-[var(--radius-card)] border border-dashed border-border-strong bg-bg-surface py-3 transition-colors duration-100 hover:border-accent-primary hover:text-text-primary"
        >
          Connect Google Calendar to see your real events here too
        </a>
      )}

      <MonthGrid items={items} events={googleEvents?.data ?? []} />

      {isPending && <Skeleton className="h-24" />}

      {isSuccess && items.length === 0 && (googleEvents?.data.length ?? 0) === 0 && (
        <EmptyState title="No deadlines coming up" action={{ label: "Browse opportunities", href: "/opportunities" }}>
          Deadlines appear here for jobs you&rsquo;ve saved or applied to, whenever the posting states one.
        </EmptyState>
      )}

      {GROUP_ORDER.filter((g) => groups.has(g)).map((groupName) => (
        <div key={groupName} className="mb-8">
          <h2 className="text-heading text-text-primary mb-3">{groupName}</h2>
          <div className="space-y-2">
            {groups.get(groupName)!.map((item) => (
              <Link
                key={item.job_group_id}
                href={`/opportunities/${item.job_group_id}`}
                className="flex items-center justify-between rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface px-5 py-3.5 hover:border-border-strong transition-colors duration-100"
              >
                <div className="min-w-0">
                  <p className="text-label font-semibold text-text-primary truncate">{item.title}</p>
                  <p className="text-caption text-text-muted mt-0.5">
                    {item.company.name} · {item.state}
                  </p>
                </div>
                <span
                  className={`text-caption shrink-0 ml-4 ${groupName === "Overdue" ? "text-danger" : "text-text-muted"}`}
                >
                  {dayLabel(item.deadline_at)}
                </span>
              </Link>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
