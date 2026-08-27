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
but not wired into any UI, and the Tailscale setup below needs root on this
machine to finish — see its own "what's left" list.

## How a phone reaches the server: Tailscale, decided 2026-08-28

[ADR-012](../../docs/adr/ADR-012-native-app-shell.md) (2026-08-07) assumed
Tailscale gave the phone a MagicDNS name to reach the laptop by.
[ADR-018](../../docs/adr/ADR-018-laptop-only-hosting.md) (2026-08-19, twelve
days later) removed Tailscale entirely: "the web dashboard is reached at
`localhost` while the stack is running," without revisiting what that means
for a physically separate phone. **Decided: Tailscale, re-added scoped to
just the phone↔laptop leg** — not a reversal of ADR-018, whose actual
reasoning (no remote host, no Oracle A1, no dead-man's switch, no public
ingress) all still holds; a tailnet between the same person's own two
devices needs none of that. Rejected the alternative (same-Wi-Fi + LAN IP,
binding `apps/api`/`apps/web` to `0.0.0.0`) because it widens reachability
to anything on the same Wi-Fi gated only by `SCOUT_AUTH_TOKEN`, and stops
working the moment the phone leaves the house — Tailscale's own device
identity is a second, independent gate, and keeps working anywhere.

`infra/compose/local.yml`'s `api`/`web` services now bind
`SCOUT_HOST_BIND_ADDR` (`.env.example`, defaults to `127.0.0.1` —
ADR-018's own posture, unchanged until this is actually set) instead of a
hardcoded `127.0.0.1`.

**What's left, and why it's not done**: starting the Tailscale daemon
needs root — a hard, unavoidable requirement for creating a network
interface on any Unix system, the same category as the Xcode/system-setup
fixes elsewhere in this project that need the user's own password. This
setup got as far as `brew install tailscale` (the CLI binary only, no
sudo needed); everything past that needs you, in order:

```bash
sudo brew services start tailscale   # or: sudo tailscaled &
sudo tailscale up                    # opens a browser — log into your Tailscale account
tailscale ip -4                      # prints this laptop's tailnet IP — stable across reboots
```

Then:

1. Set `SCOUT_HOST_BIND_ADDR` in the **repo root's** `.env` to that IP,
   `docker compose down && make live-run` (or `make dev`) to rebind.
2. Install Tailscale on the phone (Play Store), log into the **same**
   account — a phone not on the tailnet can't reach that IP regardless of
   what's configured.
3. Set `SCOUT_MOBILE_SERVER_URL=http://<that IP>:3000` before syncing (see
   Building, below) — `apps/mobile`'s own env, separate from the repo
   root's.

Optional hardening once the above works: `tailscale cert` for a real HTTPS
cert on the MagicDNS name, so `capacitor.config.ts`'s `cleartext` flag can
go back to `false`. Not required — Tailscale's own WireGuard tunnel already
encrypts the connection end-to-end regardless of whether the app-level
transport is HTTP or HTTPS, so this is about `androidScheme`/mixed-content
policy correctness, not a real exposure.

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
