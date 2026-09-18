import type { NextRequest } from "next/server";
import { proxyToApi } from "@/lib/proxy";

// GET /v1/jobs — docs/04-api-design.md section 4.1's ranked feed. Every
// app/api/* route is a thin same-origin proxy so SCOUT_AUTH_TOKEN never
// reaches browser JS; see lib/proxy.ts for the shared behavior.
export function GET(request: NextRequest) {
  return proxyToApi(request, "/v1/jobs", { search: request.nextUrl.search });
}
