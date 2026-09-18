import { proxyToApi } from "@/lib/proxy";

// POST /v1/jobs/{group_id}/state — the body is forwarded verbatim; the
// Go state machine (apps/api/internal/jobs/state.go) validates it.
export async function POST(request: Request, { params }: { params: Promise<{ groupId: string }> }) {
  const { groupId } = await params;
  return proxyToApi(request, `/v1/jobs/${encodeURIComponent(groupId)}/state`, {
    method: "POST",
    body: await request.text(),
    contentType: "application/json",
  });
}
