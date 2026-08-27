"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { initNativeBridge } from "@/lib/capacitor";

export function Providers({ children }: { children: React.ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            refetchOnWindowFocus: false,
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

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
