package com.scout.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

// Plain JVM unit test — extractSessionToken has no Android framework
// dependency, so it runs on the host without an emulator, same as the
// module's own ExampleUnitTest scaffold.
public class JobActionReceiverTest {

    @Test
    public void extractsTokenFromSoleCookie() {
        String got = JobActionReceiver.extractSessionToken("scout_web_session=abc123");
        assertEquals("abc123", got);
    }

    @Test
    public void extractsTokenWhenOtherCookiesPresent() {
        String got = JobActionReceiver.extractSessionToken(
            "other=1; scout_web_session=abc123; third=2");
        assertEquals("abc123", got);
    }

    @Test
    public void returnsNullWhenCookieAbsent() {
        assertNull(JobActionReceiver.extractSessionToken("other=1; third=2"));
    }

    @Test
    public void returnsNullForNullHeader() {
        assertNull(JobActionReceiver.extractSessionToken(null));
    }
}
