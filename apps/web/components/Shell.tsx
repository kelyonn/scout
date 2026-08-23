"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { NavIcon } from "./NavIcon";
import { COLLAPSE_COOKIE } from "@/lib/sidebar";

// Sidebar matching docs/12-frontend-ux.md section 3's information
// architecture. Every entry is real now — Companies turned out to need
// no schema work at all: the company table (infra/migrations/
// 000003_company.up.sql) already had every enrichment field, just no
// list endpoint in front of it (apps/api/internal/companies).
const NAV_SECTIONS: {
  label: string;
  icon: string;
  href?: string;
  disabled?: boolean;
}[] = [
  { label: "Home", icon: "home", href: "/" },
  { label: "Opportunities", icon: "opportunities", href: "/opportunities" },
  { label: "Saved", icon: "saved", href: "/saved" },
  { label: "Applied", icon: "applied", href: "/applied" },
  { label: "Pipeline", icon: "pipeline", href: "/pipeline" },
  { label: "Search", icon: "search", href: "/search" },
  { label: "Calendar", icon: "calendar", href: "/calendar" },
  { label: "Insights", icon: "insights", href: "/insights" },
  { label: "Companies", icon: "companies", href: "/companies" },
  { label: "Resume", icon: "resume", href: "/resume" },
  { label: "Settings", icon: "settings", href: "/settings" },
];

// initialCollapsed comes from the (app) layout, a Server Component that
// reads COLLAPSE_COOKIE via next/headers' cookies() — so the very first
// HTML the server sends already has the right width, and there's nothing
// to reconcile after hydration. The previous version read localStorage
// via useSyncExternalStore, which is hydration-safe (no mismatch warning)
// but still has no value to read until the client mounts, so a hard
// reload painted expanded for one frame and then snapped to the saved
// collapsed state — exactly the flash being fixed here.
export function Shell({
  children,
  initialCollapsed,
}: {
  children: React.ReactNode;
  initialCollapsed: boolean;
}) {
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(initialCollapsed);

  function toggle() {
    const next = !collapsed;
    setCollapsed(next);
    // Not httpOnly — this is a UI preference, not a credential, and it
    // needs to be writable from client JS. One year, same as the actual
    // session cookie's lifetime reasoning (lib/auth.ts).
    document.cookie = `${COLLAPSE_COOKIE}=${next ? "1" : "0"}; path=/; max-age=${365 * 24 * 60 * 60}; samesite=lax`;
  }

  return (
    <div className="flex min-h-screen">
      <aside
        className={`shrink-0 border-r border-border-subtle bg-bg-base flex flex-col transition-[width] duration-150 ease-[var(--ease-quick)] overflow-hidden ${
          collapsed ? "w-16" : "w-64"
        }`}
      >
        <div
          className={`flex items-center pt-8 pb-6 ${
            collapsed ? "justify-center px-0" : "justify-between px-6"
          }`}
        >
          {!collapsed && (
            <span className="text-[18px] font-semibold tracking-[-0.01em] text-text-primary whitespace-nowrap">
              Scout
            </span>
          )}
          <button
            type="button"
            onClick={toggle}
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            className="shrink-0 w-7 h-7 flex items-center justify-center rounded-md text-text-faint transition-colors duration-100 hover:text-text-primary hover:bg-bg-elevated"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              className={`transition-transform duration-150 ${collapsed ? "rotate-180" : ""}`}
            >
              <path
                d="M10.5 3L5.5 8L10.5 13"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
        </div>
        <nav className="flex-1 px-3 py-1 flex flex-col gap-1 overflow-y-auto">
          {NAV_SECTIONS.map((item) => {
            const active = item.href && (item.href === "/" ? pathname === "/" : pathname?.startsWith(item.href));
            if (item.disabled) {
              return (
                <div
                  key={item.label}
                  className={`text-label py-2.5 rounded-lg flex items-center text-text-muted cursor-not-allowed select-none ${
                    collapsed ? "justify-center px-0" : "gap-3 px-3.5"
                  }`}
                  title={`${item.label} — not built yet, needs a company registry that doesn't exist`}
                >
                  <NavIcon name={item.icon} className="shrink-0 text-text-faint" />
                  {!collapsed && item.label}
                </div>
              );
            }
            return (
              <Link
                key={item.label}
                href={item.href!}
                title={collapsed ? item.label : undefined}
                className={`text-label py-2.5 rounded-lg flex items-center transition-colors duration-100 ${
                  collapsed ? "justify-center px-0" : "gap-3 px-3.5"
                } ${
                  active
                    ? "bg-bg-elevated text-text-primary"
                    : "text-text-secondary hover:bg-bg-elevated hover:text-text-primary"
                }`}
              >
                <NavIcon name={item.icon} className={`shrink-0 ${active ? "text-accent-primary" : ""}`} />
                {!collapsed && item.label}
              </Link>
            );
          })}
        </nav>
      </aside>
      <main className="flex-1 bg-bg-base min-w-0">{children}</main>
    </div>
  );
}
