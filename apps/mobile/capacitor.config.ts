import type { CapacitorConfig } from "@capacitor/cli";

// SCOUT_MOBILE_SERVER_URL is the one genuinely open question this shell
// can't resolve on its own — see this file's own long comment below and
// HANDOFF.md's 2026-08-27 P4 entry for the full writeup. Read at config
// build time (`npx cap sync` re-reads this file), not at app runtime, so
// changing it always needs a `cap sync` + rebuild, never just an env
// change on a running install.
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
    // THE OPEN QUESTION THIS FILE CANNOT ANSWER ON ITS OWN: ADR-012
    // (2026-08-07) was written assuming Tailscale gave the phone a
    // MagicDNS name to reach the laptop by. ADR-018 (2026-08-19, twelve
    // days later) removed Tailscale entirely — "no remote host, no
    // Tailscale, no Oracle... the web dashboard is reached at localhost
    // while the stack is running" — and never revisited what that means
    // for a physically separate phone, which cannot reach another
    // device's localhost by definition. The two ADRs were never
    // reconciled; this is not an oversight in this config file, it's a
    // real unresolved architectural gap. flagged in
    // docs/19-roadmap.md's P4 section and HANDOFF.md rather than papered
    // over here.
    //
    // The default below (10.0.2.2, Android's own always-on alias for
    // "the host machine's localhost from inside the emulator") is
    // therefore deliberately scoped to emulator development ONLY — it is
    // the one case that works today, with zero network changes, and
    // needs no decision from anyone. A REAL PHONE needs one of:
    //   (a) phone and laptop on the same LAN, apps/api and apps/web
    //       bound to 0.0.0.0 instead of localhost-only (a real,
    //       deliberate loosening of ADR-018's "this process only ever
    //       binds to localhost" security posture, not a config toggle
    //       to flip casually), and SCOUT_MOBILE_SERVER_URL set to the
    //       laptop's LAN IP; or
    //   (b) Tailscale re-added for the phone<->laptop leg specifically
    //       (this does not require reversing all of ADR-018 — a tailnet
    //       between two of the user's own devices needs no Oracle host),
    //       and SCOUT_MOBILE_SERVER_URL set to the MagicDNS name.
    // Neither is decided. Set SCOUT_MOBILE_SERVER_URL before syncing for
    // real-device use once one is.
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
