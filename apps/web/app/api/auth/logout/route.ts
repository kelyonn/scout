import { NextResponse } from "next/server";
import { SESSION_COOKIE, sessionCookieOptions } from "@/lib/auth";

// Clears the session cookie on this device. The attributes must match
// the cookie being cleared or the browser keeps the original.
export function POST() {
  const res = NextResponse.json({ status: "ok" });
  res.cookies.set(SESSION_COOKIE, "", { ...sessionCookieOptions(), maxAge: 0 });
  return res;
}
