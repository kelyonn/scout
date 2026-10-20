import type { NextRequest } from "next/server";
import { proxyToApi } from "@/lib/proxy";

// GET /v1/applications — docs/04 section 4.6.
export function GET(request: NextRequest) {
  return proxyToApi(request, "/v1/applications", { search: request.nextUrl.search });
}
