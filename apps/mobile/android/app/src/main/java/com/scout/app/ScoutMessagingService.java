package com.scout.app;

import android.Manifest;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import androidx.annotation.NonNull;
import androidx.core.app.NotificationCompat;
import androidx.core.app.NotificationManagerCompat;
import androidx.core.content.ContextCompat;
import com.capacitorjs.plugins.pushnotifications.PushNotificationsPlugin;
import com.google.firebase.messaging.FirebaseMessagingService;
import com.google.firebase.messaging.RemoteMessage;
import java.util.Map;

// Replaces @capacitor/push-notifications' own MessagingService — removed in
// AndroidManifest.xml via tools:node="remove" on that exact class, because
// only one FirebaseMessagingService can actually receive
// com.google.firebase.MESSAGING_EVENT. The plugin's own service forwards
// onMessageReceived to a JS "pushNotificationReceived" listener
// (PushNotificationsPlugin.sendRemoteMessage) that apps/web/lib/
// capacitor.ts never registers, so that half is safe to drop — but its
// onNewToken forward is NOT: that's what reaches the JS "registration"
// listener capacitor.ts's own registerDevice() call depends on, so
// onNewToken below still forwards to the plugin, same as it always did.
//
// apps/notifier/internal/fcm sends every push as a data-only message
// deliberately (see fcm.go's own comment on message) so this method is
// invoked in every app state — foreground, background, or not running —
// instead of Android auto-displaying the notification itself, which can't
// carry Save/Dismiss actions and, per HANDOFF.md's own finding, silently
// falls back to Android's fcm_fallback_notification_channel when the named
// channel doesn't exist on-device yet.
public class ScoutMessagingService extends FirebaseMessagingService {

    static final String ACTION_JOB_STATE = "com.scout.app.ACTION_JOB_STATE";
    static final String EXTRA_JOB_GROUP_ID = "job_group_id";
    static final String EXTRA_STATE = "state";
    static final String EXTRA_NOTIFICATION_ID = "notification_id";

    @Override
    public void onMessageReceived(@NonNull RemoteMessage remoteMessage) {
        super.onMessageReceived(remoteMessage);

        Map<String, String> data = remoteMessage.getData();
        String title = data.get("title");
        String body = data.get("body");
        if (title == null && body == null) return; // not a Scout job push

        String channelId = data.get("channel_id");
        if (channelId == null || channelId.isEmpty()) {
            channelId = NotificationChannels.OPPORTUNITIES;
        }
        String jobGroupId = data.get("job_group_id");
        String deepLink = data.get("deep_link");
        String collapseKey = data.get("collapse_key");

        String idSeed = collapseKey != null ? collapseKey
            : jobGroupId != null ? jobGroupId
            : String.valueOf(System.currentTimeMillis());
        int notificationId = idSeed.hashCode();

        NotificationCompat.Builder builder = new NotificationCompat.Builder(this, channelId)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(new NotificationCompat.BigTextStyle().bigText(body))
            .setSmallIcon(R.mipmap.ic_launcher)
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH);

        if (deepLink != null && !deepLink.isEmpty()) {
            Intent openIntent = new Intent(Intent.ACTION_VIEW, Uri.parse(deepLink), this, MainActivity.class);
            openIntent.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
            PendingIntent contentIntent = PendingIntent.getActivity(
                this, notificationId, openIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
            builder.setContentIntent(contentIntent);
        }

        if (jobGroupId != null && !jobGroupId.isEmpty()) {
            builder.addAction(0, "Save", jobActionIntent(jobGroupId, "saved", notificationId));
            builder.addAction(0, "Dismiss", jobActionIntent(jobGroupId, "dismissed", notificationId));
        }

        // POST_NOTIFICATIONS is a runtime-revocable permission on API 33+
        // (already requested by apps/web/lib/capacitor.ts's own
        // PushNotifications.requestPermissions() on first launch, and
        // already granted per the real device it was verified against —
        // but a FirebaseMessagingService callback throwing an uncaught
        // SecurityException here would be worse than the auto-display
        // path this replaces, which the OS could never crash. Same
        // "check first, degrade if not" posture as ScoutNativePlugin's
        // own firebaseAvailable() guard.
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED) {
            return;
        }
        NotificationManagerCompat.from(this).notify(notificationId, builder.build());
    }

    @Override
    public void onNewToken(@NonNull String token) {
        super.onNewToken(token);
        // See class comment — this forward is load-bearing, not
        // boilerplate: it's what makes a rotated FCM token reach
        // apps/web/lib/capacitor.ts's registerDevice() call again.
        PushNotificationsPlugin.onNewToken(token);
    }

    // Distinct request codes per (job, action) so Save and Dismiss on the
    // same notification get independent PendingIntents instead of
    // colliding into one (which would make both buttons perform whichever
    // action was registered last).
    private PendingIntent jobActionIntent(String jobGroupId, String state, int notificationId) {
        Intent intent = new Intent(ACTION_JOB_STATE, null, this, JobActionReceiver.class);
        intent.putExtra(EXTRA_JOB_GROUP_ID, jobGroupId);
        intent.putExtra(EXTRA_STATE, state);
        intent.putExtra(EXTRA_NOTIFICATION_ID, notificationId);
        int requestCode = (jobGroupId + ":" + state).hashCode();
        return PendingIntent.getBroadcast(
            this, requestCode, intent,
            PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }
}
