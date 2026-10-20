"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { safeNextPath } from "@/lib/safeRedirect";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [token, setToken] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [showHint, setShowHint] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError(null);
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: token.trim() }),
      });
      if (res.status === 429) {
        setError("Too many attempts. Wait a few minutes, then try again.");
        return;
      }
      if (!res.ok) {
        setError("That token doesn't match. Check it and try again.");
        return;
      }
      router.replace(safeNextPath(searchParams.get("next")));
      router.refresh();
    } catch {
      setError("Couldn't reach Scout. Is the web service running?");
    } finally {
      setPending(false);
    }
  }

  return (
    <main className="min-h-screen flex items-center justify-center bg-bg-base px-4 py-8">
      <form
        onSubmit={handleSubmit}
        noValidate
        className="w-full max-w-sm rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-6 sm:p-8"
      >
        <div className="flex items-center gap-3 mb-5">
          <span
            aria-hidden
            className="w-12 h-12 flex items-center justify-center bg-accent-subtle border border-border-strong"
          >
            <svg width="32" height="32" viewBox="0 0 64 64">
              <path d="M5 32a27 27 0 1 0 54 0a27 27 0 1 0 -54 0ZM46.14 17.86L37.66 37.66L17.86 46.14L26.34 26.34Z" fill="currentColor" fillRule="evenodd" />
            </svg>
          </span>
          <div>
            <h1 className="text-title text-text-primary leading-none">Scout</h1>
            <p className="text-caption text-text-muted mt-1">Internship and new-grad roles, ranked for you.</p>
          </div>
        </div>

        <label htmlFor="token" className="block text-label text-text-secondary mb-1.5">
          Passcode
        </label>
        <input
          id="token"
          name="token"
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          autoFocus
          autoComplete="current-password"
          spellCheck={false}
          required
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? "token-error" : "token-help"}
          className="w-full text-body bg-bg-elevated border border-border-subtle rounded-[var(--radius-input)] px-3 py-2.5 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary font-mono"
        />
        <p id="token-help" className="text-caption text-text-muted mt-2">
          Enter it once on this device — you stay signed in.
        </p>

        {error && (
          <p id="token-error" role="alert" className="text-caption text-danger mt-3">
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={pending || token.trim().length === 0}
          className="w-full mt-4 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {pending ? "Checking…" : "Continue"}
        </button>

        <button
          type="button"
          onClick={() => setShowHint((s) => !s)}
          aria-expanded={showHint}
          className="mt-4 text-caption text-text-muted hover:text-text-secondary transition-colors duration-100"
        >
          Where do I find the token?
        </button>
        {showHint && (
          <p className="text-caption text-text-muted mt-2 leading-relaxed">
            Scout runs on your own machine with one access token. It&rsquo;s the{" "}
            <code className="font-mono text-text-secondary">SCOUT_AUTH_TOKEN</code> value in the server&rsquo;s{" "}
            <code className="font-mono text-text-secondary">.env</code> file. Changing that value and restarting signs
            every device out.
          </p>
        )}
      </form>
    </main>
  );
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}
