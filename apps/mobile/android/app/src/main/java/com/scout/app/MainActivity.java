package com.scout.app;

import android.os.Bundle;
import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(ScoutNativePlugin.class);
        // docs/11-notifications.md section 6.2 — must exist before a push
        // can land in the right channel instead of Android's own
        // fcm_fallback_notification_channel. See NotificationChannels'
        // own comment for why this runs on every launch.
        NotificationChannels.createAll(this);
        super.onCreate(savedInstanceState);
    }
}
