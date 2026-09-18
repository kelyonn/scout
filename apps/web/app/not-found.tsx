import Link from "next/link";

// Served for any route that doesn't exist. The (app) layout doesn't
// wrap this (Next renders the root not-found outside route groups), so
// it carries its own minimal frame rather than the sidebar.
export default function NotFound() {
  return (
    <main className="min-h-screen flex items-center justify-center bg-bg-base px-6">
      <div className="w-full max-w-md rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-8 text-center">
        <p className="text-micro text-text-muted mb-2">404</p>
        <h1 className="text-title text-text-primary">There&rsquo;s nothing here</h1>
        <p className="text-body text-text-secondary mt-2">
          The page you followed doesn&rsquo;t exist, or the link was mistyped.
        </p>
        <div className="mt-6 flex flex-wrap justify-center gap-2">
          <Link
            href="/"
            className="text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2.5 transition-colors duration-100 hover:bg-accent-hover"
          >
            Go home
          </Link>
          <Link
            href="/opportunities"
            className="text-label text-text-secondary rounded-[var(--radius-card)] border border-border-subtle px-4 py-2.5 transition-colors duration-100 hover:border-border-strong hover:text-text-primary"
          >
            Opportunities
          </Link>
        </div>
      </div>
    </main>
  );
}
