package com.scout.app;

import android.os.Bundle;
import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(ScoutNativePlugin.class);
        super.onCreate(savedInstanceState);
    }
}
