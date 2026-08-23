// Split out from components/Shell.tsx: a plain constant re-exported from
// a "use client" module doesn't reliably cross back into a Server
// Component's view of it (the RSC client-reference boundary wraps every
// export of a "use client" file, not just the component itself — an
// import of COLLAPSE_COOKIE in app/(app)/layout.tsx came through as an
// opaque reference object, not the string, and cookieStore.get() on that
// silently never matched). A plain module has no such boundary.
export const COLLAPSE_COOKIE = "scout_sidebar_collapsed";
