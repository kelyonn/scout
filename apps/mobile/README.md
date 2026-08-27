# apps/mobile

Capacitor shell wrapping `apps/web`. **Android only** — see
[ADR-012](../../docs/adr/ADR-012-native-app-shell.md).

This is not a second UI. Never fork a screen into it: `capacitor.config.ts`'s
`server.url` means the WebView loads the deployed `apps/web` build, which is
what makes a UI change reach the phone without a new binary. The native
bridge code that plugin calls actually run through
(`@capacitor/push-notifications`, `@capacitor/app`'s deep links, the badge
count) lives in `apps/web/lib/capacitor.ts` for exactly that reason — it's
part of the page the WebView loads, not part of this directory, which only
holds the native shell and its config.

## Status, 2026-08-28

Built and verified — a real debug APK, built with this repo's own Android
SDK setup (`brew install --cask android-commandlinetools` — see
`HANDOFF.md`'s 2026-08-27/28 entries), installed and run on a real (if
emulated) Android 15 device: the actual deployed `apps/web` login screen
rendered pixel-correct inside the shell, and the JS bundle executed with
zero errors (`Capacitor/Console` logcat lines confirm it, not just "the
build succeeded").

**Not yet done**: FCM push end-to-end (no Firebase project exists — see
below), the biometric-unlock and local-notification plugins are installed
but not wired into any UI, and the real-device connectivity question below
is unresolved.

## The one real open question: how does a phone reach the server?

[ADR-012](../../docs/adr/ADR-012-native-app-shell.md) (2026-08-07) assumed
Tailscale gave the phone a MagicDNS name to reach the laptop by.
[ADR-018](../../docs/adr/ADR-018-laptop-only-hosting.md) (2026-08-19, twelve
days later) removed Tailscale entirely: "the web dashboard is reached at
`localhost` while the stack is running." Nobody has reconciled the two — a
physically separate phone cannot reach another device's `localhost` by
definition, so as written, ADR-012's own architecture doesn't work for a
real phone under ADR-018's hosting model.

`capacitor.config.ts`'s `SCOUT_MOBILE_SERVER_URL` defaults to `10.0.2.2` —
the Android emulator's own always-on alias for the host machine's
`localhost`, which is why the emulator verification above worked with zero
network changes. It is **not** a real answer for a physical phone. Two real
options exist, neither decided:

- **Same Wi-Fi + LAN IP.** Bind `apps/api`/`apps/web` to `0.0.0.0` instead
  of localhost-only, point `SCOUT_MOBILE_SERVER_URL` at the laptop's LAN
  IP. A real, deliberate loosening of ADR-018's "this process only ever
  binds to localhost" security posture — not a toggle to flip without
  thinking about it, since it means anything else on the same Wi-Fi can
  also reach the API (still gated by `SCOUT_AUTH_TOKEN`, but a materially
  larger attack surface than "not reachable at all").
- **Tailscale, scoped to just the phone.** Doesn't require reversing all of
  ADR-018 — a tailnet between the user's own laptop and phone needs no
  Oracle host, no remote host at all. Point `SCOUT_MOBILE_SERVER_URL` at the
  MagicDNS name.

## Building

```bash
pnpm install
npx cap sync android          # re-run after any plugin or config change
cd android && ./gradlew assembleDebug
```

Needs `ANDROID_HOME` set (this repo's own setup:
`/opt/homebrew/share/android-commandlinetools`, via
`brew install --cask android-commandlinetools` — see the root `.zshrc`
block HANDOFF.md's 2026-08-27 entry describes) and `android/local.properties`
containing `sdk.dir=$ANDROID_HOME` (gitignored — regenerate it locally,
`cap sync` does not write it for you).

The unsigned debug APK lands at
`android/app/build/outputs/apk/debug/app-debug.apk`. **Release signing is
not set up** — the P0 Android signing keystore was never generated (still
an open P0 exit criterion; see `docs/19-roadmap.md`), so there is no
`build:release` output yet even though the script exists.

## FCM (native push)

Needs a real Firebase project — none exists yet. Once one does:

1. Firebase console → Project Settings → your Android app (`com.scout.app`)
   → download `google-services.json` into `apps/mobile/android/app/`
   (gitignored, never commit the real one).
2. Project Settings → Service Accounts → "Generate new private key" → set
   `SCOUT_FCM_PROJECT_ID`/`SCOUT_FCM_SERVICE_ACCOUNT_JSON` in the
   **notifier's** own `.env` (see `.env.example`) — a completely separate
   step from (1), since the service account is what
   `apps/notifier/internal/fcm` uses to *send*, while `google-services.json`
   is what this app uses to *receive*.

Until both exist, `apps/notifier/internal/fcm.Client` stays disabled (by
design — see that package's own doc comment) and this app's own
`PushNotifications.register()` call will fail without a configured Firebase
project to register against; `apps/web/lib/capacitor.ts` logs that failure
and continues rather than blocking the rest of the app.
