import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE, isValidToken } from "@/lib/auth";
import { safeNextPath } from "@/lib/safeRedirect";

// Next.js 16 renamed middleware.ts to proxy.ts (same mechanism, new file
// name). This is the frontend half of ADR-015's auth design: the Go
// API's own bearer-token gate (apps/api/internal/auth) was always built,
// but nothing ever stopped a browser that could reach the Next.js origin
// from using the app/api/* proxy routes, which hold SCOUT_AUTH_TOKEN
// server-side and use it unconditionally. Every route except the few
// listed in the matcher below requires a valid session cookie first.
export function proxy(request: NextRequest) {
  const cookie = request.cookies.get(SESSION_COOKIE)?.value ?? "";
  if (isValidToken(cookie)) {
    return NextResponse.next();
  }

  if (request.nextUrl.pathname.startsWith("/api/")) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const loginUrl = new URL("/login", request.url);
  const next = safeNextPath(request.nextUrl.pathname + request.nextUrl.search);
  if (next !== "/") loginUrl.searchParams.set("next", next);
  return NextResponse.redirect(loginUrl);
}

export const config = {
  // Public: the login page and its API, the container healthcheck,
  // Next's own static assets, and the PWA/icon files a browser fetches
  // before (or without) a session.
  matcher: [
    "/((?!login|api/auth/login|api/health|_next/static|_next/image|favicon.ico|icon.svg|apple-icon|manifest.webmanifest).*)",
  ],
};
