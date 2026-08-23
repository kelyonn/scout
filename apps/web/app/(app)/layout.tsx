import { cookies } from "next/headers";
import { Shell } from "@/components/Shell";
import { COLLAPSE_COOKIE } from "@/lib/sidebar";

// Every route under this group is an authenticated app page — proxy.ts is
// the actual gate (it runs before this layout and redirects to /login),
// this just gives the whole group its persistent chrome in one place so
// Shell mounts once for the session instead of once per page. A Server
// Component (not "use client") specifically so it can read the sidebar's
// collapsed cookie before the first paint — see Shell.tsx's own comment
// on why that's what kills the reload flash.
export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const initialCollapsed = cookieStore.get(COLLAPSE_COOKIE)?.value === "1";

  return <Shell initialCollapsed={initialCollapsed}>{children}</Shell>;
}
