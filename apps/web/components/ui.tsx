import Link from "next/link";
import { errorMessage } from "@/lib/api";

// The handful of building blocks every screen repeats: a page header, a
// loading skeleton, an empty state with a next step, and an error notice
// with a retry. One definition each so the states look and behave the
// same on every page instead of drifting per file.

export function PageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="mb-6 sm:mb-8 flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-title text-text-primary">{title}</h1>
        {subtitle && <p className="text-caption text-text-muted mt-1.5">{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}

export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={`rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface animate-pulse ${className}`}
    />
  );
}

export function CardListSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div className="space-y-4" role="status" aria-label="Loading">
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton key={i} className="h-32" />
      ))}
    </div>
  );
}

export function ErrorNotice({
  error,
  fallback,
  onRetry,
}: {
  error: unknown;
  fallback?: string;
  onRetry?: () => void;
}) {
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center justify-between gap-3 text-body text-text-primary rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 px-4 py-3.5"
    >
      <span className="min-w-0">{errorMessage(error, fallback)}</span>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="text-label shrink-0 rounded-[var(--radius-input)] border border-danger/30 px-3 py-1.5 text-danger transition-colors duration-100 hover:bg-danger/10"
        >
          Retry
        </button>
      )}
    </div>
  );
}

export function EmptyState({
  title,
  children,
  action,
}: {
  title: string;
  children?: React.ReactNode;
  action?: { label: string; href: string } | React.ReactNode;
}) {
  const isLink = action && typeof action === "object" && "href" in action && "label" in action;
  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-6 sm:p-8 text-center">
      <p className="text-heading text-text-primary">{title}</p>
      {children && <div className="text-body text-text-secondary mt-2 max-w-md mx-auto">{children}</div>}
      {action &&
        (isLink ? (
          <Link
            href={action.href}
            className="inline-block mt-5 text-label text-white bg-accent-primary rounded-[var(--radius-card)] px-4 py-2.5 transition-colors duration-100 hover:bg-accent-hover"
          >
            {action.label}
          </Link>
        ) : (
          <div className="mt-5">{action}</div>
        ))}
    </div>
  );
}
