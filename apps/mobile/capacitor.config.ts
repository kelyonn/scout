import type { CapacitorConfig } from "@capacitor/cli";

// SCOUT_MOBILE_SERVER_URL — updated 2026-10-21 for ADR-019's cardless
// hosted pipeline: points at the public Vercel deployment
// (https://scout-pi-sepia.vercel.app), not a laptop or a tailnet
// address. Read at config build time (`npx cap sync` re-reads this
// file), not at app runtime, so changing it always needs a `cap sync`
// + rebuild, never just an env change on a running install. Defaults
// to the Android emulator's own host-loopback alias when unset, which
// needs no network setup at all — only relevant for local emulator
// development against `make dev`, never for a real device anymore.
const serverURL = process.env.SCOUT_MOBILE_SERVER_URL || "http://10.0.2.2:3000";

const config: CapacitorConfig = {
  // Arbitrary — this app is never published to the Play Store (ADR-012:
  // "no fee, no store, no review"), so appId only needs to be unique on
  // the device it's sideloaded onto, not a domain Scout actually owns.
  appId: "com.scout.app",
  appName: "Scout",
  // webDir points at a real but minimal directory (see www/index.html's
  // own comment) — server.url below means this bundled content is only
  // ever the offline fallback, never what a connected user actually sees.
  webDir: "www",
  server: {
    // ADR-012's whole architecture: "WebView → the deployed Next.js app
    // (identical UI to the browser, no fork)". This is that pointer.
    //
    // SUPERSEDED HISTORY: ADR-012 (2026-08-07) assumed Tailscale gave the
    // phone a MagicDNS name to reach the laptop by. ADR-018 (2026-08-19)
    // removed Tailscale/the remote host entirely for the backend, and a
    // 2026-08-28 patch re-added Tailscale scoped to just the phone<->
    // laptop leg so the app could still reach a laptop-hosted backend.
    // ADR-019 (2026-10-21) replaced the laptop-hosted backend with the
    // cardless hosted pipeline (GitHub Actions + Neon + Upstash + Render
    // + Vercel), which has a real public URL — so the phone<->laptop
    // Tailscale leg this comment used to document is gone too. The app
    // now just points at Vercel directly, same as any other internet
    // client; SCOUT_AUTH_TOKEN / SCOUT_LOGIN_PASSCODE (apps/web/lib/
    // auth.ts) remain the only gate, same as the browser dashboard.
    url: serverURL,
    cleartext: serverURL.startsWith("http://"),
  },
  android: {
    // The FCM data payload's own deep link (apps/notifier/internal/
    // deliver.renderPushNotification: "scout://job/{id}") is handled by
    // @capacitor/app's appUrlOpen listener in the web app itself
    // (apps/web/lib/capacitor.ts), not by an intent-filter here — Capacitor
    // routes a custom-scheme URL opened while the app is already running
    // to that listener automatically once App is installed as a plugin.
    allowMixedContent: false,
  },
};

export default config;
