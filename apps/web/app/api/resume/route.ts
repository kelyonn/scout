import { proxyToApi } from "@/lib/proxy";

// GET/POST /v1/resume. The multipart body passes straight through — this
// route never parses the PDF, the Go handler does. AGENTS.md rule 9: the
// resume goes to the Go API on this host and nowhere else.
export function GET(request: Request) {
  return proxyToApi(request, "/v1/resume");
}

export async function POST(request: Request) {
  return proxyToApi(request, "/v1/resume", {
    method: "POST",
    body: await request.arrayBuffer(),
    contentType: request.headers.get("content-type") ?? "application/octet-stream",
  });
}
