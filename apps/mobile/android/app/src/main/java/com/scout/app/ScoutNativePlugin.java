package com.scout.app;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import com.google.firebase.messaging.FirebaseMessaging;

// Exists for one reason: @capacitor/push-notifications' Android register()
// calls FirebaseMessaging.getInstance() with no try/catch, which throws an
// uncaught IllegalStateException on the CapacitorPlugins thread when no
// google-services.json was bundled (build.gradle only applies the
// google-services plugin when that file exists — see app/build.gradle).
// That crash kills the whole process before any JS promise ever rejects,
// so no amount of try/catch in apps/web/lib/capacitor.ts can catch it —
// the check has to happen natively, before that call is ever made.
@CapacitorPlugin(name = "ScoutNative")
public class ScoutNativePlugin extends Plugin {
    @PluginMethod
    public void firebaseAvailable(PluginCall call) {
        boolean available;
        try {
            // Same call PushNotificationsPlugin.register() makes — if this
            // throws here, it would have thrown (and crashed the process)
            // there too.
            FirebaseMessaging.getInstance();
            available = true;
        } catch (IllegalStateException e) {
            available = false;
        }
        JSObject result = new JSObject();
        result.put("available", available);
        call.resolve(result);
    }
}
