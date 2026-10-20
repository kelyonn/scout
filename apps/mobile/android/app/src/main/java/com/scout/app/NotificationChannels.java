package com.scout.app;

import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Context;
import android.os.Build;

// docs/11-notifications.md section 6.2's two live channel IDs ("opportunities"
// and "deadlines" — the only two any code path in apps/notifier actually
// sends to; "digests" and "system" are reserved but unused, per that
// section's own note). Created directly via NotificationManager rather than
// through either bundled Capacitor plugin's own channel-manager utility,
// since neither one creates domain-specific channels on its own — nothing
// outside node_modules called createNotificationChannel() before this,
// which is why a real push landed on Android's generic
// fcm_fallback_notification_channel instead of either of these.
//
// Channels are created on every launch, not just the first — cheap and
// idempotent: recreating an existing channel updates its name/description
// but never its importance once the user has seen it (Android's own
// lock-in, see that section's "cannot be changed afterwards" note), so this
// can't silently re-tune a channel the user already adjusted.
final class NotificationChannels {
    static final String OPPORTUNITIES = "opportunities";
    static final String DEADLINES = "deadlines";

    private NotificationChannels() {}

    static void createAll(Context context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return;
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        if (manager == null) return;

        NotificationChannel opportunities = new NotificationChannel(
            OPPORTUNITIES, "Opportunities", NotificationManager.IMPORTANCE_HIGH);
        opportunities.setDescription("Instant triggers");
        manager.createNotificationChannel(opportunities);

        NotificationChannel deadlines = new NotificationChannel(
            DEADLINES, "Deadlines", NotificationManager.IMPORTANCE_HIGH);
        deadlines.setDescription("T-72h and T-24h warnings");
        manager.createNotificationChannel(deadlines);
    }
}
