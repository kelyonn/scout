"use client";

import { useSyncExternalStore } from "react";

function subscribe(onChange: () => void): () => void {
  window.addEventListener("online", onChange);
  window.addEventListener("offline", onChange);
  return () => {
    window.removeEventListener("online", onChange);
    window.removeEventListener("offline", onChange);
  };
}

// navigator.onLine is a coarse signal (true only means "not definitely
// offline"), which is exactly the right strength for a banner: it never
// hides a real outage, and a false "offline" clears itself the moment the
// browser knows better. Server render assumes online so there's no flash.
function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
}

export function OfflineBanner() {
  const online = useOnline();
  if (online) return null;
  return (
    <div role="status" className="border-b border-warning/30 bg-warning/10 px-4 py-2 text-center text-caption text-warning">
      You&rsquo;re offline. Scout will pick up where you left off once the connection is back.
    </div>
  );
}
