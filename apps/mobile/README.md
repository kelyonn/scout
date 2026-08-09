# apps/mobile

Capacitor shell wrapping `apps/web`. **Android only** — see
[ADR-012](../../docs/adr/ADR-012-native-app-shell.md).

**Not implemented yet.** Scheduled for M3.

This is not a second UI. Never fork a screen into it: the WebView loads the
deployed `apps/web` build, which is what makes a UI change reach the phone
without a new binary.
