"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

// Sidebar matching docs/12-frontend-ux.md section 3's information
// architecture — but only the sections with a real, working backend
// (Opportunities, Pipeline, Search, Resume) are enabled. Calendar,
// Companies, and Insights need state this schema doesn't track yet (a
// company registry, interview scheduling, analytics aggregation) —
// shown, disabled, rather than either hidden (which would misrepresent
// the intended product shape) or faked with dead links.
const NAV_SECTIONS: {
  label: string;
  href?: string;
  disabled?: boolean;
}[] = [
  { label: "Opportunities", href: "/opportunities" },
  { label: "Pipeline", href: "/pipeline" },
  { label: "Search", href: "/search" },
  { label: "Calendar", disabled: true },
  { label: "Companies", disabled: true },
  { label: "Insights", disabled: true },
  { label: "Resume", href: "/resume" },
  { label: "Settings", disabled: true },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r border-border-subtle bg-bg-surface flex flex-col">
        <div className="px-4 py-5">
          <span className="text-heading text-text-primary">Scout</span>
        </div>
        <nav className="flex-1 px-2 space-y-0.5">
          {NAV_SECTIONS.map((item) => {
            const active = item.href && pathname?.startsWith(item.href);
            if (item.disabled) {
              return (
                <div
                  key={item.label}
                  className="text-label px-3 py-2 rounded-[var(--radius-card)] text-text-muted cursor-not-allowed select-none"
                  title="Not built yet — needs backend that doesn't exist"
                >
                  {item.label}
                </div>
              );
            }
            return (
              <Link
                key={item.label}
                href={item.href!}
                className={`block text-label px-3 py-2 rounded-[var(--radius-card)] transition-colors duration-100 ${
                  active
                    ? "bg-accent-subtle text-accent-primary"
                    : "text-text-secondary hover:bg-bg-elevated hover:text-text-primary"
                }`}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>
      <main className="flex-1 bg-bg-base min-w-0">{children}</main>
    </div>
  );
}
