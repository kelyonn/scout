import type { NextRequest } from "next/server";
import { proxyToApi } from "@/lib/proxy";

// GET /v1/companies
export function GET(request: NextRequest) {
  return proxyToApi(request, "/v1/companies", { search: request.nextUrl.search });
}
