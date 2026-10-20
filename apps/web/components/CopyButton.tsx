"use client";

import { useState } from "react";

// Small copy-to-clipboard control for the profile page — copying a phone
// number or a bullet into an application form is the page's main job.
export function CopyButton({ text, label = "Copy", onDark = false }: { text: string; label?: string; onDark?: boolean }) {
  const [done, setDone] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      setTimeout(() => setDone(false), 1400);
    } catch {
      // Clipboard can be refused (insecure origin, permissions); nothing to recover.
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      aria-label={`${label}: ${text.slice(0, 40)}`}
      className={`text-micro border px-2 py-0.5 shrink-0 hover:bg-accent-subtle hover:text-text-primary ${
        onDark ? "text-bg-base border-bg-base" : "text-text-muted border-border-subtle"
      }`}
    >
      {done ? "Copied" : label}
    </button>
  );
}
