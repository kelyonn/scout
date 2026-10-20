import { proxyToApi } from "@/lib/proxy";

// GET /v1/jobs/{group_id}
export async function GET(request: Request, { params }: { params: Promise<{ groupId: string }> }) {
  const { groupId } = await params;
  return proxyToApi(request, `/v1/jobs/${encodeURIComponent(groupId)}`);
}
