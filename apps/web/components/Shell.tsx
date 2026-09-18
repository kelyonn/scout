"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { NavIcon } from "./NavIcon";
import { OfflineBanner } from "./OfflineBanner";
import { COLLAPSE_COOKIE } from "@/lib/sidebar";

// Sidebar matching docs/12-frontend-ux.md section 3's information
// architecture. Every entry is real — nothing here is a placeholder for
// a screen that doesn't exist.
const NAV_SECTIONS: { label: string; icon: string; href: string }[] = [
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

// docs/12 section 6: "Bottom tabs, 5 items, 48px targets" — the five
// most-used destinations. Everything else is one tap away via Home's
// jump list or the mobile header's settings link.
const MOBILE_TABS = NAV_SECTIONS.filter((s) =>
  ["/", "/opportunities", "/pipeline", "/calendar", "/search"].includes(s.href),
);

function isActive(pathname: string | null, href: string): boolean {
  if (!pathname) return false;
  return href === "/" ? pathname === "/" : pathname.startsWith(href);
}

// initialCollapsed comes from the (app) layout, a Server Component that
// reads COLLAPSE_COOKIE via next/headers' cookies() — so the very first
// HTML the server sends already has the right width, and there's nothing
// to reconcile after hydration.
export function Shell({ children, initialCollapsed }: { children: React.ReactNode; initialCollapsed: boolean }) {
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
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-[1001] focus:rounded-[var(--radius-input)] focus:bg-accent-primary focus:px-3 focus:py-2 focus:text-label focus:text-white"
      >
        Skip to content
      </a>

      {/* Desktop sidebar */}
      <aside
        aria-label="Primary"
        className={`hidden md:flex shrink-0 sticky top-0 h-screen border-r border-border-subtle bg-bg-base flex-col transition-[width] duration-150 ease-[var(--ease-quick)] overflow-hidden ${
          collapsed ? "w-16" : "w-64"
        }`}
      >
        <div className={`flex items-center pt-8 pb-6 ${collapsed ? "justify-center px-0" : "justify-between px-6"}`}>
          {!collapsed && (
            <Link href="/" className="text-[18px] font-semibold tracking-[-0.01em] text-text-primary whitespace-nowrap">
              Scout
            </Link>
          )}
          <button
            type="button"
            onClick={toggle}
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            aria-expanded={!collapsed}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            className="shrink-0 w-7 h-7 flex items-center justify-center rounded-md text-text-faint transition-colors duration-100 hover:text-text-primary hover:bg-bg-elevated"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              className={`transition-transform duration-150 ${collapsed ? "rotate-180" : ""}`}
              aria-hidden
            >
              <path d="M10.5 3L5.5 8L10.5 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        </div>
        <nav className="flex-1 px-3 py-1 flex flex-col gap-1 overflow-y-auto">
          {NAV_SECTIONS.map((item) => {
            const active = isActive(pathname, item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                title={collapsed ? item.label : undefined}
                aria-current={active ? "page" : undefined}
                className={`text-label py-2.5 rounded-lg flex items-center transition-colors duration-100 ${
                  collapsed ? "justify-center px-0" : "gap-3 px-3.5"
                } ${active ? "bg-bg-elevated text-text-primary" : "text-text-secondary hover:bg-bg-elevated hover:text-text-primary"}`}
              >
                <NavIcon name={item.icon} className={`shrink-0 ${active ? "text-accent-primary" : ""}`} />
                {!collapsed && item.label}
              </Link>
            );
          })}
        </nav>
      </aside>

      <div className="flex-1 min-w-0 flex flex-col">
        {/* Mobile header */}
        <header className="md:hidden sticky top-0 z-40 flex items-center justify-between border-b border-border-subtle bg-bg-base/95 backdrop-blur px-4 pt-[env(safe-area-inset-top)] h-[calc(52px+env(safe-area-inset-top))]">
          <Link href="/" className="text-[17px] font-semibold tracking-[-0.01em] text-text-primary">
            Scout
          </Link>
          <Link
            href="/settings"
            aria-label="Settings"
            aria-current={isActive(pathname, "/settings") ? "page" : undefined}
            className={`w-10 h-10 -mr-2 flex items-center justify-center rounded-lg transition-colors duration-100 hover:bg-bg-elevated ${
              isActive(pathname, "/settings") ? "text-accent-primary" : "text-text-muted"
            }`}
          >
            <NavIcon name="settings" />
          </Link>
        </header>

        <OfflineBanner />

        <main
          id="main"
          className="flex-1 bg-bg-base min-w-0 pb-[calc(var(--bottom-nav-height)+env(safe-area-inset-bottom))] md:pb-0"
        >
          {children}
        </main>
      </div>

      {/* Mobile bottom tabs */}
      <nav
        aria-label="Primary"
        className="md:hidden fixed bottom-0 inset-x-0 z-40 border-t border-border-subtle bg-bg-base/95 backdrop-blur pb-[env(safe-area-inset-bottom)]"
      >
        <ul className="grid grid-cols-5 h-[var(--bottom-nav-height)]">
          {MOBILE_TABS.map((item) => {
            const active = isActive(pathname, item.href);
            return (
              <li key={item.href}>
                <Link
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={`flex h-full flex-col items-center justify-center gap-1 text-[10px] font-medium tracking-[0.02em] transition-colors duration-100 ${
                    active ? "text-accent-primary" : "text-text-muted hover:text-text-primary"
                  }`}
                >
                  <NavIcon name={item.icon} className="shrink-0" />
                  {item.label}
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>
    </div>
  );
}
