// Package fcm implements the notifier's Firebase Cloud Messaging client —
// ADR-012's native_push transport, the "Android, app installed" row in
// that ADR's transport-selection table.
//
// FCM's HTTP v1 API (the only one Google still supports — the legacy
// server-key API this project's docs originally assumed is deprecated)
// authenticates with a service-account OAuth2 token, not a static key.
// golang.org/x/oauth2/google is already a dependency (apps/api/internal/
// googlecalendar uses the same module for its own, differently-shaped
// three-legged flow), so this adds no new supply-chain surface — just a
// different sub-package of one already-justified module.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// requestTimeout bounds one send call.
const requestTimeout = 15 * time.Second

const defaultAPIBase = "https://fcm.googleapis.com"

// fcmScope is the one OAuth2 scope FCM sends need — deliberately not the
// broader Firebase or Cloud Platform scopes a service account key could
// also be used for elsewhere, so a leaked token from this process can only
// ever send push notifications, nothing else.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// Client sends push notifications via FCM's HTTP v1 API.
//
// A Client with no project ID or credentials is valid and does nothing —
// same "empty token, method returns an error immediately" posture
// apps/notifier/internal/telegram.Client and apps/collector/internal/
// heartbeat.Pinger already use for "not configured" being a normal local
// development state, not a startup failure.
type Client struct {
	projectID string
	apiBase   string
	http      *http.Client // nil when not configured
}

// New returns a Client for the given Firebase project, authenticating with
// serviceAccountJSON (the raw JSON key downloaded from the Firebase
// console — Project Settings > Service Accounts > Generate new private
// key). Either argument empty means "not configured": Enabled reports
// false and Send refuses rather than attempting a request that could only
// fail. credErr from a malformed (but non-empty) JSON blob is folded into
// "not configured" too, logged by the caller rather than returned here —
// mirrors how a mistyped SCOUT_HEALTHCHECK_INTERVAL degrades instead of
// blocking startup elsewhere in this codebase.
func New(ctx context.Context, projectID, serviceAccountJSON string) (*Client, error) {
	if projectID == "" || serviceAccountJSON == "" {
		return &Client{}, nil
	}

	// The type-specific variant, not the deprecated google.CredentialsFromJSON
	// — this operator-supplied env var is always meant to be a service
	// account key, and rejecting anything else (a user OAuth client
	// config, say) rather than silently accepting whatever credential
	// type happens to parse is exactly the validation the deprecation
	// notice asks callers to do themselves.
	creds, err := google.CredentialsFromJSONWithTypeAndParams(ctx, []byte(serviceAccountJSON), google.ServiceAccount,
		google.CredentialsParams{Scopes: []string{fcmScope}})
	if err != nil {
		return nil, fmt.Errorf("fcm: parse service account json: %w", err)
	}

	return &Client{
		projectID: projectID,
		apiBase:   defaultAPIBase,
		http:      oauth2HTTPClient(ctx, creds.TokenSource),
	}, nil
}

// NewWithBaseURL builds a Client that skips real OAuth2 entirely —
// httpClient and baseURL point at an httptest.Server instead of Google's
// real token endpoint and FCM itself. Exported for the same reason
// apps/notifier/internal/telegram.NewWithBaseURL is: apps/notifier/
// internal/deliver's own tests, in a different package, construct a fake
// push channel this way.
func NewWithBaseURL(projectID, baseURL string, httpClient *http.Client) *Client {
	return &Client{projectID: projectID, apiBase: baseURL, http: httpClient}
}

// oauth2HTTPClient wraps an oauth2.TokenSource in an *http.Client with a
// fixed request timeout — oauth2.NewClient's own result has none, and a
// hung token refresh or FCM call must not block a delivery run
// indefinitely, same reasoning apps/notifier/internal/telegram.Client's
// own fixed-timeout http.Client documents for sendMessage.
func oauth2HTTPClient(ctx context.Context, ts oauth2.TokenSource) *http.Client {
	client := oauth2.NewClient(ctx, ts)
	client.Timeout = requestTimeout
	return client
}

// Enabled reports whether a project and service account were configured.
func (c *Client) Enabled() bool { return c.http != nil }

// message is FCM HTTP v1's request body — the subset this notifier needs.
//
// Deliberately data-only: no top-level "notification" block. A
// notification+data message is auto-displayed by Android's own FCM SDK
// whenever the app is backgrounded, which (a) uses whatever channel the
// device already has cached, silently falling back to Android's own
// fcm_fallback_notification_channel if the named one doesn't exist yet,
// and (b) can't carry the Save/Dismiss actions docs/11 section 6.2
// requires. Data-only messages instead always reach the app's own
// FirebaseMessagingService.onMessageReceived (com.scout.app.
// ScoutMessagingService — apps/mobile/android/app/src/main/java/com/scout/
// app/), which builds the notification itself with the right channel and
// actions, in every app state. Title/body/channel_id travel inside Data
// for exactly this reason; see Send below.
type message struct {
	Message struct {
		Token   string            `json:"token"`
		Data    map[string]string `json:"data,omitempty"`
		Android *androidConfig    `json:"android,omitempty"`
	} `json:"message"`
}

// androidConfig carries FCM's own delivery-priority and collapse
// semantics — distinct from the notification channel, which now travels
// as an ordinary data field (see message's own comment) since there is no
// "android.notification" block on a data-only message for it to live in.
type androidConfig struct {
	Priority    string `json:"priority,omitempty"` // "high" or "normal"
	CollapseKey string `json:"collapse_key,omitempty"`
}

// Notification is Send's input — a transport-agnostic shape the caller
// (apps/notifier/internal/deliver) builds once and reuses across channels,
// the same way it already builds one rendered string for Telegram.
type Notification struct {
	Title        string
	Body         string
	ChannelID    string            // Android notification channel — docs/12's four channels
	CollapseKey  string            // e.g. the job_group_id, so a rescored update replaces rather than stacks
	Data         map[string]string // deep-link target, notification id, etc. — read by the shell, not shown
	HighPriority bool
}

// sendResponse is FCM's own success envelope — {"name":
// "projects/{id}/messages/{message_id}"}.
type sendResponse struct {
	Name string `json:"name"`
}

// fcmErrorResponse is FCM's own error envelope on a non-2xx response.
type fcmErrorResponse struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// Send delivers one notification to deviceToken. Returns FCM's own message
// name (the trailing message id) for notification_delivery.provider_msg_id.
//
// An "UNREGISTERED" or "NOT_FOUND" status — the device uninstalled the app
// or the token rotated without a fresh registration — comes back as a
// typed *TokenInvalidError so the caller can retire the channel row instead
// of retrying it forever (docs/17 section 10's chaos test: "Return
// UNREGISTERED for every push token... tokens retired, user told on other
// channels, no silent loss").
func (c *Client) Send(ctx context.Context, deviceToken string, n Notification) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("fcm: not configured (no project id or service account)")
	}

	data := make(map[string]string, len(n.Data)+3)
	for k, v := range n.Data {
		data[k] = v
	}
	data["title"] = n.Title
	data["body"] = n.Body
	if n.ChannelID != "" {
		data["channel_id"] = n.ChannelID
	}

	var body message
	body.Message.Token = deviceToken
	body.Message.Data = data

	android := &androidConfig{CollapseKey: n.CollapseKey}
	if n.HighPriority {
		android.Priority = "high"
	} else {
		android.Priority = "normal"
	}
	body.Message.Android = android

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("fcm: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/projects/%s/messages:send", c.apiBase, c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("fcm: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope fcmErrorResponse
		_ = json.Unmarshal(respBody, &envelope)
		if envelope.Error.Status == "UNREGISTERED" || envelope.Error.Status == "NOT_FOUND" {
			return "", &TokenInvalidError{Status: envelope.Error.Status}
		}
		return "", fmt.Errorf("fcm: api error (status %d): %s", resp.StatusCode, envelope.Error.Message)
	}

	var result sendResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("fcm: decode response: %w", err)
	}
	return result.Name, nil
}

// TokenInvalidError signals a device token FCM will never accept again —
// see Send's own comment on the chaos-test requirement this exists for.
type TokenInvalidError struct {
	Status string
}

func (e *TokenInvalidError) Error() string {
	return fmt.Sprintf("fcm: device token invalid (%s)", e.Status)
}
