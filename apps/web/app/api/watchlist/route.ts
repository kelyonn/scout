import { proxyToApi } from "@/lib/proxy";

// GET/POST /v1/me/watchlist
export function GET(request: Request) {
  return proxyToApi(request, "/v1/me/watchlist");
}

export async function POST(request: Request) {
  return proxyToApi(request, "/v1/me/watchlist", {
    method: "POST",
    body: await request.text(),
    contentType: "application/json",
  });
}
