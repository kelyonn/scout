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

## Status, 2026-08-29

**Verified on a real physical phone, not just the emulator.** The
2026-08-28 entry below proved the shell on an emulator via `10.0.2.2`;
this session closed the actual gap that entry left open — Tailscale is
up (`sudo tailscale up` needed root, which a prior session didn't have;
this one did), `SCOUT_HOST_BIND_ADDR` in the repo root's `.env` is set to
the laptop's tailnet IP, and a real debug APK built with
`SCOUT_MOBILE_SERVER_URL` pointed at that IP was installed on a real
Pixel 7 over `adb` (wireless debugging) and launched. `logcat` confirms a
clean load — `Loading app at http://<tailnet-ip>:3000`, `App started`,
`App resumed`, `ScoutNative.firebaseAvailable()` correctly returning
`{"available":false}` and the JS side skipping push registration without
crashing — and a screenshot shows the actual dashboard (not just the
login screen this time), pixel-correct, over the tailnet. Zero fatal
exceptions.

One real snag worth recording: Android's wireless-debugging `adb`
connection only auto-reconnects over the *same local Wi-Fi network* as
the host, independent of Tailscale — when the phone dropped to cellular
mid-session, `adb` lost the phone entirely (Tailscale itself stayed
reachable, proven by a successful `ping` over cellular) until the phone
was back on the same Wi-Fi. This only affects the development/debugging
control channel, not the app's own connectivity to the server, which
Tailscale carries over any network.

**The P0 release keystore now exists.** Generated this session
(RSA-4096, valid to 2054), stored outside the repo entirely at
`~/.scout-secrets/android/scout-release.jks` per
[docs/13-security-privacy.md](../../docs/13-security-privacy.md)'s "SOPS
+ offline media" guidance for never-rotate secrets — deliberately, in one
session, as that doc asks, not as an incidental side effect. Wired into
`android/app/build.gradle` via a gitignored `android/keystore.properties`
pointer file (same "load if present, degrade if not" shape as the
`google-services.json` block already there). `npm run build:release` now
produces a real signed APK, confirmed via `apksigner verify
--print-certs`. **You still need to copy the keystore file and its
password file to offline media (a USB drive) yourself** — that step
needs a human with a USB drive, not more code.

**FCM is now fully wired, same session.** A real Firebase project
(`scout-d46e6`) already existed under the user's account from an earlier,
otherwise-untouched attempt; an Android app (`com.scout.app`) was
registered against it, `google-services.json` downloaded into
`apps/mobile/android/app/` (gitignored), and a service account key
generated for the notifier (stored outside the repo at
`~/.scout-secrets/fcm/`, minified to one line into `SCOUT_FCM_SERVICE_ACCOUNT_JSON`
per `.env.example`'s own instructions). Verified live, not just wired:
notifier logs show `"native push armed","transport":"fcm"`, and a debug
APK rebuilt with `google-services.json` present produced a real FCM
token on the same Pixel 7 (`ScoutNative.firebaseAvailable()` now returns
`true`, permissions granted, `PushNotifications.register()` returns a
token), which reached `POST /v1/push/register-device` and landed as a
real `notification_channel` row (`kind='native_push', platform='android'`).
Sending an actual push through that token to confirm delivery end-to-end
hasn't been done yet — that needs a real trigger event, not just
registration.

**Not yet done**: the biometric-unlock and local-notification plugins are
installed but not wired into any UI.

## How a phone reaches the server: Tailscale, decided 2026-08-28, verified 2026-08-29

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

**Done, 2026-08-29.** The daemon needed root to start (the same category
as any Xcode/system-setup fix that needs the user's own password), which
is why the 2026-08-28 session left this open — that got done since. The
laptop's tailnet IP is `100.78.200.69` (`tailscale ip -4`), set as
`SCOUT_HOST_BIND_ADDR` in the repo root's `.env`. The phone (`pixel-7`)
is already installed and paired to the same tailnet account. `apps/mobile/.env`
(gitignored, separate from the repo root's) carries
`SCOUT_MOBILE_SERVER_URL=http://100.78.200.69:3000`; export it before
`cap sync` if that file changes, since `capacitor.config.ts` reads
`process.env` directly at sync time with no dotenv loader — the `.env`
file is a convenience to source, not auto-loaded.

One thing worth knowing if this breaks again: Tailscale on the phone can
show "offline" in `tailscale status` from the laptop even when a
connection is about to succeed — it lagged behind the phone's actual
state by up to a minute during setup. Confirm from the phone's own
Tailscale app (should say "Connected") rather than trusting the laptop's
view if something seems stuck.

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
now set up.** The P0 keystore was generated 2026-08-29
(`~/.scout-secrets/android/scout-release.jks`, outside the repo — never
rotates, see docs/13 §"Secret rotation"), with `android/keystore.properties`
(gitignored) as the pointer `build.gradle` reads. Without that file
present, `pnpm run build:release` still runs but produces an *unsigned*
release APK — the signing block in `build.gradle` degrades gracefully,
same shape as the `google-services.json` check below it. With the file
present, it produces a real signed APK at
`android/app/build/outputs/apk/release/app-release.apk`
(`apksigner verify --print-certs` confirms the signer).

## FCM (native push)

**Done, 2026-08-29.** Firebase project `scout-d46e6` exists (Spark plan,
no-cost), with the Android app registered and both halves in place:

1. `apps/mobile/android/app/google-services.json` — this app's own copy
   (gitignored, never committed). Lets `PushNotifications.register()`
   actually get a token instead of crashing (`ScoutNativePlugin`'s guard
   made that failure mode graceful before this existed; now it's moot).
2. The notifier's `SCOUT_FCM_PROJECT_ID`/`SCOUT_FCM_SERVICE_ACCOUNT_JSON`
   (repo root `.env`, never committed — the JSON is minified to one line
   per `.env.example`'s own instructions; the source key file lives at
   `~/.scout-secrets/fcm/scout-fcm-service-account.json`, outside the
   repo, same posture as the Android keystore).

Verified live: notifier logs `"native push armed","transport":"fcm"` on
startup, and a real device (`apps/mobile`'s debug build on a Pixel 7)
registered a real FCM token that landed in `notification_channel` via
`POST /v1/push/register-device`. **Not yet verified**: an actual push
*delivered* through that token — registration works, but nothing has
triggered a real notification send through it yet.
