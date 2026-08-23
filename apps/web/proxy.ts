import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE, isValidToken } from "@/lib/auth";

// Next.js 16 renamed middleware.ts to proxy.ts (same mechanism, new file
// name — see node_modules/next/dist/docs/01-app/03-api-reference/
// 03-file-conventions/proxy.md). This is the frontend half of ADR-015's
// auth design: the Go API's own bearer-token gate
// (apps/api/internal/auth) was always built, but nothing ever stopped a
// browser that could reach the Next.js origin from using the app/api/*
// proxy routes, which hold SCOUT_AUTH_TOKEN server-side and use it
// unconditionally. This closes that gap — every route except /login and
// the login API itself now requires a valid session cookie first.
export function proxy(request: NextRequest) {
  const cookie = request.cookies.get(SESSION_COOKIE)?.value ?? "";
  if (isValidToken(cookie)) {
    return NextResponse.next();
  }

  if (request.nextUrl.pathname.startsWith("/api/")) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const loginUrl = new URL("/login", request.url);
  loginUrl.searchParams.set("next", request.nextUrl.pathname);
  return NextResponse.redirect(loginUrl);
}

export const config = {
  matcher: [
    "/((?!login|api/auth/login|_next/static|_next/image|favicon.ico).*)",
  ],
};
