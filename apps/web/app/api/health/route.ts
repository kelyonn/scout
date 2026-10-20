// Shallow liveness for the container healthcheck (infra/compose/*.yml)
// — the process is up and can serve a request. Unauthenticated, like
// the Go API's own /health, and exempted in proxy.ts's matcher. It
// deliberately does not probe the Go API: a healthcheck that fails when
// a *different* container is down makes Compose restart the wrong one.
export function GET() {
  return Response.json({ status: "ok" });
}
