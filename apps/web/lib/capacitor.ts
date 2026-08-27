"use client";

// ADR-012's native bridge — the code that actually runs when this same
// app is loaded inside apps/mobile's WebView instead of a normal browser.
// This has to live in apps/web, not apps/mobile: capacitor.config.ts's
// server.url means apps/mobile's own bundled www/ is never what a
// connected user sees (see that file's own comment), so any plugin call
// (register for push, read the App state, set the badge) only ever
// executes if it's part of the page actually being loaded — this one.
// Capacitor.isNativePlatform() is false in every normal desktop/mobile
// browser, so none of this runs or has any effect outside the shell.
import { App } from "@capacitor/app";
import { Capacitor } from "@capacitor/core";
import { PushNotifications } from "@capacitor/push-notifications";
import { Badge } from "@capawesome/capacitor-badge";

import { registerDevice } from "./api";

// jobDeepLinkPath extracts "/opportunities/{id}" from a
// "scout://job/{id}" URL — apps/notifier/internal/deliver.
// renderPushNotification's own Data["deep_link"], the same string a
// notification tap or (via appUrlOpen) an already-running app hands this
// listener. Returns null for anything else, so a malformed or
// unrecognized scheme is a no-op rather than a bad navigation.
export function jobDeepLinkPath(url: string): string | null {
  const match = /^scout:\/\/job\/([^/?#]+)/.exec(url);
  if (!match) return null;
  return `/opportunities/${match[1]}`;
}

// initNativeBridge wires push registration, deep links, and the badge
// count. Called once from app/providers.tsx; every step is independently
// best-effort — a rejected permission or a registration-endpoint failure
// logs and stops that one step rather than throwing, since none of this
// is on the critical path for the page the user is actually looking at.
export function initNativeBridge(navigate: (path: string) => void): void {
  if (!Capacitor.isNativePlatform()) return;

  void registerForPush();
  void listenForDeepLinks(navigate);
}

async function registerForPush(): Promise<void> {
  try {
    const perm = await PushNotifications.requestPermissions();
    if (perm.receive !== "granted") {
      console.warn("scout: push permission not granted", perm.receive);
      return;
    }

    // The 'registration' listener fires with the real FCM token once
    // Firebase hands one back — asynchronous relative to register()
    // itself, so this is added before calling it, not after.
    PushNotifications.addListener("registration", (token) => {
      void registerDevice({
        platform: Capacitor.getPlatform() === "ios" ? "ios" : "android",
        device_token: token.value,
        app_version: APP_VERSION,
      }).catch((err: unknown) => {
        console.error("scout: register-device failed", err);
      });
    });

    PushNotifications.addListener("registrationError", (err) => {
      console.error("scout: push registration error", err);
    });

    await PushNotifications.register();
  } catch (err) {
    console.error("scout: push setup failed", err);
  }
}

// APP_VERSION has no real value to report yet — apps/mobile's
// package.json version, not something the web bundle can read at build
// time without a bundler config change neither app currently has. Left
// undefined (registerDevice's own field is optional) rather than
// hardcoded and silently wrong the first time apps/mobile's version
// actually changes.
const APP_VERSION: string | undefined = undefined;

async function listenForDeepLinks(navigate: (path: string) => void): Promise<void> {
  // Covers both cases docs/11 section 6.2's own worked example implies:
  // the app was already running (appUrlOpen fires directly) and the app
  // was launched cold by tapping the notification (getLaunchUrl catches
  // the URL the OS handed the process at startup, which appUrlOpen alone
  // would miss).
  App.addListener("appUrlOpen", (data) => {
    const path = jobDeepLinkPath(data.url);
    if (path) navigate(path);
  });

  try {
    const launch = await App.getLaunchUrl();
    if (launch?.url) {
      const path = jobDeepLinkPath(launch.url);
      if (path) navigate(path);
    }
  } catch (err) {
    console.error("scout: get launch url failed", err);
  }
}

// setBadgeCount mirrors docs/11 section 6.2's own "notification_count"
// field — called from wherever the unread/new count is already computed
// for the Home page (apps/web/app/(app)/page.tsx's own funnel counts),
// not computed independently here. A no-op outside the native shell, and
// swallows its own errors: a stale badge is a cosmetic gap, never worth
// surfacing to the user as an error.
export async function setBadgeCount(count: number): Promise<void> {
  if (!Capacitor.isNativePlatform()) return;
  try {
    await Badge.set({ count });
  } catch (err) {
    console.error("scout: set badge failed", err);
  }
}
