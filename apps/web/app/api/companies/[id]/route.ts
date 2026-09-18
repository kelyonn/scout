import { proxyToApi } from "@/lib/proxy";

// GET /v1/companies/{id}
export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return proxyToApi(request, `/v1/companies/${encodeURIComponent(id)}`);
}
