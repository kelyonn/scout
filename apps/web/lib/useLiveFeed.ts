"use client";

import { useEffect, useRef } from "react";

interface JobNewEvent {
  job_group_id: string;
  priority: number;
  title: string;
}

// docs/04-api-design.md section 4.3's SSE stream, consumed via the
// browser's native EventSource — auto-reconnect and Last-Event-ID replay
// on reconnect are both built into EventSource itself, so this hook is
// just wiring, not a reimplementation of either. See app/api/stream/
// route.ts for why a same-origin proxy exists (EventSource can't send a
// custom Authorization header).
export function useLiveFeed(onJobNew: (job: JobNewEvent) => void) {
  const handlerRef = useRef(onJobNew);

  // Refs may not be mutated during render (React flags this — reads/writes
  // to `.current` must happen in an effect or event handler, not the
  // component body). This effect keeps the ref pointed at the latest
  // callback without re-subscribing the EventSource below on every
  // caller re-render.
  useEffect(() => {
    handlerRef.current = onJobNew;
  }, [onJobNew]);

  useEffect(() => {
    const source = new EventSource("/api/stream");
    const listener = (e: MessageEvent) => {
      try {
        handlerRef.current(JSON.parse(e.data));
      } catch {
        // A malformed payload degrades to "no live update this time,"
        // not a crashed page.
      }
    };
    source.addEventListener("job.new", listener);
    return () => {
      source.removeEventListener("job.new", listener);
      source.close();
    };
  }, []);
}
