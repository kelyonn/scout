"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { disconnectGoogleCalendar, errorMessage, fetchGoogleCalendarStatus, syncGoogleCalendar } from "@/lib/api";
import { useToast } from "@/components/Toast";
import { ErrorNotice, PageHeader, Skeleton } from "@/components/ui";

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 text-body py-3 border-b border-border-subtle last:border-0">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary font-mono tabular-nums text-right">{value}</span>
    </div>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-5 mb-6">
      <h2 className="text-heading text-text-primary mb-1">{title}</h2>
      {children}
    </section>
  );
}

function GoogleCalendarSection() {
  const searchParams = useSearchParams();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const callbackResult = searchParams.get("google");

  const { data: status, isPending, isError, error, refetch } = useQuery({
    queryKey: ["google-calendar-status"],
    queryFn: fetchGoogleCalendarStatus,
  });

  const sync = useMutation({
    mutationFn: syncGoogleCalendar,
    onError: (err) => toast(errorMessage(err, "Sync failed."), { kind: "error" }),
  });

  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const disconnect = useMutation({
    mutationFn: disconnectGoogleCalendar,
    onSuccess: () => {
      setConfirmDisconnect(false);
      queryClient.invalidateQueries({ queryKey: ["google-calendar-status"] });
      queryClient.invalidateQueries({ queryKey: ["google-calendar-events"] });
      toast("Google Calendar disconnected");
    },
    onError: (err) => toast(errorMessage(err, "Couldn't disconnect."), { kind: "error" }),
  });

  if (isPending) {
    return (
      <Card title="Google Calendar">
        <Skeleton className="h-16 mt-2" />
      </Card>
    );
  }

  if (isError) {
    return (
      <Card title="Google Calendar">
        <div className="mt-2">
          <ErrorNotice error={error} fallback="Couldn't check the calendar connection." onRetry={() => void refetch()} />
        </div>
      </Card>
    );
  }

  if (!status?.enabled) {
    return (
      <Card title="Google Calendar">
        <p className="text-caption text-text-muted">
          Not set up on this installation. To enable it, create OAuth credentials in Google Cloud Console and set{" "}
          <code className="font-mono text-text-secondary">SCOUT_GOOGLE_CLIENT_ID</code> and{" "}
          <code className="font-mono text-text-secondary">SCOUT_GOOGLE_CLIENT_SECRET</code> in the server&rsquo;s{" "}
          <code className="font-mono text-text-secondary">.env</code> — the steps are in{" "}
          <code className="font-mono text-text-secondary">.env.example</code>.
        </p>
      </Card>
    );
  }

  return (
    <Card title="Google Calendar">
      <p className="text-caption text-text-muted mb-3">
        Pushes tracked deadlines out as real calendar events, and shows your existing events alongside them on the Calendar
        page.
      </p>

      {callbackResult === "connected" && (
        <p role="status" className="text-caption text-success mb-3">
          Connected successfully.
        </p>
      )}
      {callbackResult === "error" && (
        <p role="alert" className="text-caption text-danger mb-3">
          Connection failed. The redirect URI must match exactly what&rsquo;s registered in Google Cloud Console.
        </p>
      )}
      {callbackResult === "unreachable" && (
        <p role="alert" className="text-caption text-danger mb-3">
          Scout&rsquo;s API wasn&rsquo;t reachable during the connection. Check it&rsquo;s running and try again.
        </p>
      )}

      {status.connected ? (
        <>
          <Row label="Status" value="Connected" />
          <Row label="Calendar" value={status.calendar_id ?? "primary"} />
          {status.connected_at && <Row label="Connected" value={new Date(status.connected_at).toLocaleDateString()} />}

          {sync.isSuccess && (
            <p role="status" className="text-caption text-success mt-3">
              Pushed {sync.data.pushed} deadline{sync.data.pushed === 1 ? "" : "s"}.
              {sync.data.errors && sync.data.errors.length > 0 && ` ${sync.data.errors.length} failed.`}
            </p>
          )}

          <div className="flex flex-col sm:flex-row gap-2 mt-4">
            <button
              type="button"
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
              className="flex-1 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50"
            >
              {sync.isPending ? "Syncing…" : "Sync now"}
            </button>
            {confirmDisconnect ? (
              <div className="flex-1 flex gap-2">
                <button
                  type="button"
                  onClick={() => disconnect.mutate()}
                  disabled={disconnect.isPending}
                  className="flex-1 text-label text-white bg-danger rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:opacity-90 disabled:opacity-50"
                >
                  {disconnect.isPending ? "Disconnecting…" : "Yes, disconnect"}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmDisconnect(false)}
                  className="text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle px-3 py-2.5 transition-colors duration-100 hover:text-text-primary"
                >
                  Keep
                </button>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => setConfirmDisconnect(true)}
                className="flex-1 text-label text-danger border border-danger/30 rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-danger/10"
              >
                Disconnect
              </button>
            )}
          </div>
          {confirmDisconnect && (
            <p className="text-caption text-text-muted mt-2">
              Events already pushed to Google stay there; Scout just stops syncing and forgets its access.
            </p>
          )}
        </>
      ) : (
        <a
          href="/api/integrations/google/connect"
          className="block text-center text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover"
        >
          Connect Google Calendar
        </a>
      )}
    </Card>
  );
}

function SettingsContent() {
  const router = useRouter();
  const { toast } = useToast();
  const [loggingOut, setLoggingOut] = useState(false);

  async function handleLogout() {
    setLoggingOut(true);
    try {
      const res = await fetch("/api/auth/logout", { method: "POST" });
      if (!res.ok) throw new Error("logout failed");
      router.replace("/login");
      router.refresh();
    } catch {
      setLoggingOut(false);
      toast("Couldn't sign out. Check the connection and try again.", { kind: "error" });
    }
  }

  return (
    <div className="max-w-2xl mx-auto page">
      <PageHeader title="Settings" subtitle="Scout is a single-person tool: one account, one access token, your data on your own machine." />

      <Card title="This device">
        <Row label="Signed in" value="Yes" />
        <Row label="Sign-in method" value="Access token" />
        <p className="text-caption text-text-muted mt-3">
          Signing out only affects this browser. To sign out everywhere, change{" "}
          <code className="font-mono text-text-secondary">SCOUT_AUTH_TOKEN</code> on the server and restart it.
        </p>
        <button
          type="button"
          onClick={() => void handleLogout()}
          disabled={loggingOut}
          className="w-full mt-4 text-label text-danger border border-danger/30 rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-danger/10 disabled:opacity-50"
        >
          {loggingOut ? "Signing out…" : "Sign out on this device"}
        </button>
      </Card>

      <GoogleCalendarSection />

      <Card title="Notifications and scoring">
        <p className="text-caption text-text-muted">
          Notification channels (Telegram, the Android app), quiet hours, and the scoring weights are configured on the
          server, not here — see the README&rsquo;s configuration section. There is no in-app editor for them yet.
        </p>
      </Card>
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
