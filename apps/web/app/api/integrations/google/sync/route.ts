import { proxyToApi } from "@/lib/proxy";

export function POST(request: Request) {
  return proxyToApi(request, "/v1/integrations/google/sync", { method: "POST" });
}
