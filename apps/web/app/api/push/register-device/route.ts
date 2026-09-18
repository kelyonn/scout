import { proxyToApi } from "@/lib/proxy";

// POST /v1/push/register-device — called only from inside apps/mobile's
// WebView (lib/capacitor.ts) once Firebase hands the app an FCM token.
export async function POST(request: Request) {
  return proxyToApi(request, "/v1/push/register-device", {
    method: "POST",
    body: await request.text(),
    contentType: "application/json",
  });
}
