"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  disconnectGoogleCalendar,
  fetchGoogleCalendarStatus,
  syncGoogleCalendar,
} from "@/lib/api";

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between text-body py-3 border-b border-border-subtle last:border-0">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary font-mono tabular-nums">{value}</span>
    </div>
  );
}

function GoogleCalendarSection() {
  const searchParams = useSearchParams();
  const queryClient = useQueryClient();
  const callbackResult = searchParams.get("google");

  const { data: status, isLoading } = useQuery({
    queryKey: ["google-calendar-status"],
    queryFn: fetchGoogleCalendarStatus,
  });

  const sync = useMutation({
    mutationFn: syncGoogleCalendar,
  });

  const disconnect = useMutation({
    mutationFn: disconnectGoogleCalendar,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["google-calendar-status"] }),
  });

  if (isLoading) {
    return (
      <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5 mb-6">
        <p className="text-body text-text-muted">Loading…</p>
      </div>
    );
  }

  if (!status?.enabled) {
    return (
      <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5 mb-6">
        <h2 className="text-heading text-text-primary mb-1">Google Calendar</h2>
        <p className="text-caption text-text-muted">
          Not configured on this deployment — needs SCOUT_GOOGLE_CLIENT_ID
          and SCOUT_GOOGLE_CLIENT_SECRET set (see .env.example for the
          Google Cloud Console setup steps).
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5 mb-6">
      <h2 className="text-heading text-text-primary mb-1">Google Calendar</h2>
      <p className="text-caption text-text-muted mb-3">
        Pushes tracked deadlines out as real events, and pulls your existing
        events in for the Calendar page to show alongside them.
      </p>

      {callbackResult === "connected" && (
        <p className="text-caption text-success mb-3">Connected successfully.</p>
      )}
      {callbackResult === "error" && (
        <p className="text-caption text-danger mb-3">
          Connection failed — check the redirect URI matches exactly what&rsquo;s registered in Google Cloud Console.
        </p>
      )}

      {status.connected ? (
        <>
          <Row label="Status" value="Connected" />
          <Row label="Calendar" value={status.calendar_id ?? "primary"} />
          {status.connected_at && (
            <Row label="Connected" value={new Date(status.connected_at).toLocaleDateString()} />
          )}

          {sync.isSuccess && (
            <p className="text-caption text-success mt-3">
              Pushed {sync.data.pushed} deadline{sync.data.pushed === 1 ? "" : "s"}.
              {sync.data.errors && sync.data.errors.length > 0 && ` ${sync.data.errors.length} failed.`}
            </p>
          )}
          {sync.isError && (
            <p className="text-caption text-danger mt-3">
              Sync failed: {sync.error instanceof Error ? sync.error.message : "unknown error"}
            </p>
          )}

          <div className="flex gap-2 mt-4">
            <button
              type="button"
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
              className="flex-1 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50"
            >
              {sync.isPending ? "Syncing…" : "Sync now"}
            </button>
            <button
              type="button"
              onClick={() => disconnect.mutate()}
              disabled={disconnect.isPending}
              className="flex-1 text-label text-danger border border-danger/30 rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-danger/10 disabled:opacity-50"
            >
              {disconnect.isPending ? "Disconnecting…" : "Disconnect"}
            </button>
          </div>
        </>
      ) : (
        <a
          href="/api/integrations/google/connect"
          className="block text-center text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover"
        >
          Connect Google Calendar
        </a>
      )}
    </div>
  );
}

function SettingsContent() {
  const router = useRouter();
  const [loggingOut, setLoggingOut] = useState(false);

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      router.replace("/login");
      router.refresh();
    }
  }

  return (
    <div className="max-w-2xl mx-auto px-10 py-10">
      <div className="mb-8">
        <h1 className="text-title text-text-primary">Settings</h1>
        <p className="text-caption text-text-muted mt-1.5">
          Scout has exactly one user (docs/02-architecture.md §6) — no accounts, no
          preferences server, just this device&rsquo;s session and a few local
          toggles.
        </p>
      </div>

      <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5 mb-6">
        <h2 className="text-heading text-text-primary mb-1">Session</h2>
        <Row label="Signed in" value="Yes, this device" />
        <Row label="Auth" value="Bearer token (ADR-015)" />
        <button
          type="button"
          onClick={handleLogout}
          disabled={loggingOut}
          className="w-full mt-4 text-label text-danger border border-danger/30 rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-danger/10 disabled:opacity-50"
        >
          {loggingOut ? "Signing out…" : "Sign out on this device"}
        </button>
      </div>

      <GoogleCalendarSection />

      <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5">
        <h2 className="text-heading text-text-primary mb-1">Not built yet</h2>
        <p className="text-caption text-text-muted">
          Notification channels, quiet hours, and scoring weight tuning are
          real backend features (docs/09, docs/11) with no settings UI in
          front of them yet — they&rsquo;re edited via config today, not here.
        </p>
      </div>
    </div>
  );
}

export default function SettingsPage() {
  return (
    <Suspense>
      <SettingsContent />
    </Suspense>
  );
}
