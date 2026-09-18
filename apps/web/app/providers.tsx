"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { ToastProvider } from "@/components/Toast";
import { ApiError } from "@/lib/api";
import { initNativeBridge } from "@/lib/capacitor";

export function Providers({ children }: { children: React.ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            refetchOnWindowFocus: false,
            // One retry, and only for a network-level failure (status 0):
            // a 4xx won't change (a 404 stays a 404, a 401 has already
            // bounced the browser to /login), and a 5xx from the proxy
            // means the API is down — a second try a second later just
            // delays the error notice and its Retry button. TanStack also
            // pauses retries while the tab is hidden, so fewer automatic
            // retries means fewer "Loading…" screens that are really a
            // paused failure.
            retry: (failureCount, error) => {
              if (error instanceof ApiError && error.status !== 0) return false;
              return failureCount < 1;
            },
          },
        },
      }),
  );

  const router = useRouter();
  useEffect(() => {
    // A no-op outside apps/mobile's own WebView — see lib/capacitor.ts's
    // own comment. Runs once per app load, not per navigation: push
    // registration and the appUrlOpen listener both need to happen
    // exactly once for the process's lifetime, not once per route.
    initNativeBridge((path) => router.push(path));
    // router omitted deliberately — it's stable for the component's
    // lifetime, and re-running this on every router identity change
    // would re-register push and double-add the appUrlOpen listener.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <QueryClientProvider client={client}>
      <ToastProvider>{children}</ToastProvider>
    </QueryClientProvider>
  );
}
