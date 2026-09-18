"use client";

import { useEffect } from "react";
import Link from "next/link";

// The route-level error boundary: a thrown render error on any page lands
// here instead of a blank screen. Nothing about the error itself is shown
// — a stack trace or a message like "Cannot read properties of undefined"
// helps nobody at this layer — but it is logged for whoever is debugging.
export default function ErrorPage({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  useEffect(() => {
    console.error("scout: page error", error.digest ?? error.name);
  }, [error]);

  return (
    <main className="min-h-screen flex items-center justify-center bg-bg-base px-6">
      <div className="w-full max-w-md rounded-[var(--radius-card)] border border-danger/30 bg-bg-surface p-8 text-center">
        <h1 className="text-title text-text-primary">Something went wrong</h1>
        <p className="text-body text-text-secondary mt-2">
          This page hit an error it couldn&rsquo;t recover from. Trying again usually works; if it keeps
          happening, the API service may be down.
        </p>
        {error.digest && <p className="text-caption text-text-faint mt-3 font-mono">ref {error.digest}</p>}
        <div className="mt-6 flex flex-wrap justify-center gap-2">
          <button
            type="button"
            onClick={reset}
            className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2.5 transition-colors duration-100 hover:bg-accent-hover"
          >
            Try again
          </button>
          <Link
            href="/"
            className="text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle px-4 py-2.5 transition-colors duration-100 hover:border-border-strong hover:text-text-primary"
          >
            Go home
          </Link>
        </div>
      </div>
    </main>
  );
}
