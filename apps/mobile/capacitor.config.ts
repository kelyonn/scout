import type { CapacitorConfig } from "@capacitor/cli";

// SCOUT_MOBILE_SERVER_URL — decided 2026-08-28: Tailscale, scoped to just
// the phone<->laptop leg, not a reversal of ADR-018 (see the server block
// below for the full reasoning). Read at config build time (`npx cap
// sync` re-reads this file), not at app runtime, so changing it always
// needs a `cap sync` + rebuild, never just an env change on a running
// install. Defaults to the Android emulator's own host-loopback alias,
// which needs no Tailscale at all — set this for a real device.
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
    // THE GAP AND THE DECISION: ADR-012 (2026-08-07) assumed Tailscale
    // gave the phone a MagicDNS name to reach the laptop by. ADR-018
    // (2026-08-19) removed Tailscale entirely — "no remote host, no
    // Tailscale, no Oracle... the web dashboard is reached at localhost
    // while the stack is running" — without revisiting what that means
    // for a physically separate phone. Decided 2026-08-28: **Tailscale,
    // re-added scoped to just the phone<->laptop leg.** This is not a
    // reversal of ADR-018 — that ADR's reasoning was about not needing a
    // remote HOST (no Oracle A1, no dead-man's switch, no public
    // ingress), all of which stays true. A tailnet between two of the
    // same person's own devices needs none of that; it's the same shape
    // as any other personal-device mesh. Rejected the alternative (LAN
    // IP + binding apps/api/apps/web to 0.0.0.0) because that widens
    // reachability to literally anything on the same Wi-Fi gated only by
    // SCOUT_AUTH_TOKEN, whereas Tailscale's own device identity is a
    // second, independent gate — and it keeps working away from home
    // Wi-Fi, which a LAN IP never would.
    //
    // infra/compose/local.yml's api/web services now bind
    // SCOUT_HOST_BIND_ADDR (.env.example's own setup steps) instead of a
    // hardcoded 127.0.0.1 — set it to this laptop's Tailscale IP
    // (`tailscale ip -4`) to actually finish this. Until that's done,
    // the default below (10.0.2.2, Android's own always-on alias for
    // "the host machine's localhost from inside the emulator") is what's
    // actually reachable — real for emulator development, not a stand-in
    // for a real phone.
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
