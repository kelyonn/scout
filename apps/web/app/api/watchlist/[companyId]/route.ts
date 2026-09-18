import { proxyToApi } from "@/lib/proxy";

// DELETE /v1/me/watchlist/{company_id}
export async function DELETE(request: Request, { params }: { params: Promise<{ companyId: string }> }) {
  const { companyId } = await params;
  return proxyToApi(request, `/v1/me/watchlist/${encodeURIComponent(companyId)}`, { method: "DELETE" });
}
