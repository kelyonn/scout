import type { NextRequest } from "next/server";
import { proxyToApi } from "@/lib/proxy";

// GET /v1/search — docs/04 section 4.2.
export function GET(request: NextRequest) {
  return proxyToApi(request, "/v1/search", { search: request.nextUrl.search });
}
