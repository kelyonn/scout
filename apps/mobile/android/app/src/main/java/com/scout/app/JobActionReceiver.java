package com.scout.app;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;
import android.webkit.CookieManager;
import androidx.core.app.NotificationManagerCompat;
import com.getcapacitor.CapConfig;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

// Handles a Save/Dismiss tap on a Scout job notification without opening
// the app — docs/19-roadmap.md P4's "Save/Dismiss work from the lock
// screen" exit criterion, docs/11-notifications.md section 6.2's own
// "actions call the API from the native layer."
//
// POSTs to the same /v1/jobs/{group_id}/state the WebView already calls
// (via apps/web/app/api/jobs/[groupId]/state/route.ts), authenticated the
// same way the WebView is: the scout_web_session cookie apps/web/app/api/
// auth/login/route.ts sets on sign-in. That route's own comment records
// that the cookie's *value* is the raw SCOUT_AUTH_TOKEN, not an opaque
// session id — so reading it back here is reading the real bearer
// credential, not inventing a second one. Read via CookieManager, which
// persists to disk independently of any live Bridge/WebView instance, so
// this works even when a push arrives with the app not running. If the
// user has never signed in through the app once, there is no cookie yet;
// this silently no-ops rather than surfacing an error the user has no way
// to act on from a notification — the notification's own tap-to-open
// still works, and signing in once fixes actions going forward.
public class JobActionReceiver extends BroadcastReceiver {
    private static final String TAG = "ScoutJobAction";
    private static final Pattern SESSION_COOKIE = Pattern.compile("scout_web_session=([^;]+)");
    private static final int TIMEOUT_MS = 10_000;

    @Override
    public void onReceive(Context context, Intent intent) {
        String jobGroupId = intent.getStringExtra(ScoutMessagingService.EXTRA_JOB_GROUP_ID);
        String state = intent.getStringExtra(ScoutMessagingService.EXTRA_STATE);
        int notificationId = intent.getIntExtra(ScoutMessagingService.EXTRA_NOTIFICATION_ID, -1);
        if (jobGroupId == null || state == null) return;

        String serverUrl = CapConfig.loadDefault(context).getServerUrl();
        if (serverUrl == null || serverUrl.isEmpty()) {
            Log.w(TAG, "no server url configured, dropping job action");
            return;
        }

        String token = extractSessionToken(CookieManager.getInstance().getCookie(serverUrl));
        if (token == null) {
            Log.w(TAG, "no session cookie yet — open the app once to sign in before actions work");
            return;
        }

        // Real network I/O, so this can't run on the receiver's own
        // (main) thread — goAsync() keeps the process alive past
        // onReceive's return until finish() is called.
        BroadcastReceiver.PendingResult pending = goAsync();
        Context appContext = context.getApplicationContext();
        new Thread(() -> {
            try {
                // Mirrors apps/web/components/StateControls.tsx's own
                // transitionTo(): the state machine
                // (apps/api/internal/jobs/state.go) only allows
                // new -> viewed/dismissed directly, so "saved" from a
                // job the user has never opened 409s unless "viewed" is
                // sent first. The browser UI already knows this and
                // chains the hop client-side; a lock-screen tap has no
                // equivalent "the user just looked at the card" step, so
                // this does the same chain here. Best-effort and ignored
                // if the job's already past "viewed" (that attempt 409s
                // harmlessly; only the real target's result matters).
                // Confirmed live 2026-08-29: without this, tapping
                // "Save" on a never-opened job's notification 409'd
                // every time.
                if ("saved".equals(state)) {
                    postState(serverUrl, jobGroupId, "viewed", token);
                }
                int code = postState(serverUrl, jobGroupId, state, token);
                boolean ok = code >= 200 && code < 300;
                if (ok && notificationId != -1) {
                    NotificationManagerCompat.from(appContext).cancel(notificationId);
                }
            } catch (Exception e) {
                Log.e(TAG, "job state update failed", e);
            } finally {
                pending.finish();
            }
        }).start();
    }

    // Package-private rather than private so JobActionReceiverTest (a plain
    // JVM unit test — no Android framework dependency here) can exercise it
    // directly, matching this module's own src/test/ convention.
    static String extractSessionToken(String cookieHeader) {
        if (cookieHeader == null) return null;
        Matcher m = SESSION_COOKIE.matcher(cookieHeader);
        return m.find() ? m.group(1) : null;
    }

    // Returns the response's HTTP status code (never throws for a non-2xx
    // response — a 409 from the intermediate "viewed" hop is expected and
    // handled by the caller, not an error path).
    private static int postState(String serverUrl, String jobGroupId, String state, String token) throws Exception {
        URL url = new URL(serverUrl + "/api/jobs/" + jobGroupId + "/state");
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        try {
            conn.setRequestMethod("POST");
            conn.setRequestProperty("Content-Type", "application/json");
            conn.setRequestProperty("Cookie", "scout_web_session=" + token);
            conn.setConnectTimeout(TIMEOUT_MS);
            conn.setReadTimeout(TIMEOUT_MS);
            conn.setDoOutput(true);

            String requestBody = "{\"state\":\"" + state + "\"}";
            try (OutputStream out = conn.getOutputStream()) {
                out.write(requestBody.getBytes(StandardCharsets.UTF_8));
            }

            int code = conn.getResponseCode();
            if (code < 200 || code >= 300) {
                Log.w(TAG, "job state update to '" + state + "' rejected: HTTP " + code);
            }
            return code;
        } finally {
            conn.disconnect();
        }
    }
}
