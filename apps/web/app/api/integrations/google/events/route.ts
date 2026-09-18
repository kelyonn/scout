import { proxyToApi } from "@/lib/proxy";

export function GET(request: Request) {
  return proxyToApi(request, "/v1/integrations/google/events");
}
