"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [token, setToken] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
      });
      if (!res.ok) {
        setError("That token doesn't match. Check .env's SCOUT_AUTH_TOKEN.");
        return;
      }
      router.replace(searchParams.get("next") || "/");
      router.refresh();
    } catch {
      setError("Couldn't reach the server — is it running?");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-base px-6">
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-sm rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8"
      >
        <h1 className="text-title text-text-primary">Scout</h1>
        <p className="text-caption text-text-muted mt-1.5 mb-6">
          Enter your access token to continue. One device, once — it stays signed in.
        </p>

        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="Access token"
          autoFocus
          className="w-full text-body bg-bg-elevated border border-border-subtle rounded-[var(--radius-input)] px-3 py-2.5 text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary font-mono"
        />

        {error && <p className="text-caption text-danger mt-3">{error}</p>}

        <button
          type="submit"
          disabled={pending || token.length === 0}
          className="w-full mt-4 text-label text-white bg-accent-primary rounded-[var(--radius-card)] py-2.5 transition-colors duration-100 hover:bg-accent-hover disabled:opacity-50"
        >
          {pending ? "Checking…" : "Continue"}
        </button>
      </form>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}
