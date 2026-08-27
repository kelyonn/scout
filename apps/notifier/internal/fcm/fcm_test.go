package fcm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew_EmptyConfigIsDisabled(t *testing.T) {
	c, err := New(context.Background(), "", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Enabled() {
		t.Error("Enabled() = true, want false with no project id or service account")
	}
}

func TestNew_ProjectIDWithoutServiceAccountIsDisabled(t *testing.T) {
	c, err := New(context.Background(), "scout-app", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Enabled() {
		t.Error("Enabled() = true, want false with no service account")
	}
}

func TestNew_MalformedServiceAccountJSONErrors(t *testing.T) {
	_, err := New(context.Background(), "scout-app", "not valid json")
	if err == nil {
		t.Fatal("expected an error for malformed service account JSON, got nil")
	}
}

func TestSend_NotConfiguredRefuses(t *testing.T) {
	c := &Client{}
	_, err := c.Send(context.Background(), "some-device-token", Notification{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error from a disabled client, got nil")
	}
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewWithBaseURL("scout-app", srv.URL, srv.Client())
}

func TestSend_Success(t *testing.T) {
	var gotBody message
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/scout-app/messages:send" {
			t.Errorf("path = %q, want /v1/projects/scout-app/messages:send", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sendResponse{Name: "projects/scout-app/messages/abc123"})
	})

	msgID, err := c.Send(context.Background(), "device-token-1", Notification{
		Title: "Bengaluru match — 92", Body: "Senior SRE at Amtech",
		ChannelID: "bengaluru_match", CollapseKey: "job-group-1", HighPriority: true,
		Data: map[string]string{"job_group_id": "job-group-1"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msgID != "projects/scout-app/messages/abc123" {
		t.Errorf("msgID = %q", msgID)
	}

	if gotBody.Message.Token != "device-token-1" {
		t.Errorf("Token = %q", gotBody.Message.Token)
	}
	if gotBody.Message.Notification.Title != "Bengaluru match — 92" {
		t.Errorf("Notification.Title = %q", gotBody.Message.Notification.Title)
	}
	if gotBody.Message.Android == nil || gotBody.Message.Android.Priority != "high" {
		t.Errorf("Android.Priority = %+v, want high", gotBody.Message.Android)
	}
	if gotBody.Message.Android.Notification == nil || gotBody.Message.Android.Notification.ChannelID != "bengaluru_match" {
		t.Errorf("Android.Notification.ChannelID = %+v", gotBody.Message.Android.Notification)
	}
	if gotBody.Message.Android.CollapseKey != "job-group-1" {
		t.Errorf("CollapseKey = %q", gotBody.Message.Android.CollapseKey)
	}
	if gotBody.Message.Data["job_group_id"] != "job-group-1" {
		t.Errorf("Data = %+v", gotBody.Message.Data)
	}
}

func TestSend_LowPriorityByDefault(t *testing.T) {
	var gotBody message
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sendResponse{Name: "x"})
	})
	if _, err := c.Send(context.Background(), "tok", Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotBody.Message.Android.Priority != "normal" {
		t.Errorf("Priority = %q, want normal when HighPriority is false", gotBody.Message.Android.Priority)
	}
}

// TestSend_UnregisteredTokenReturnsTypedError is docs/17 section 10's own
// chaos-test requirement: "Return UNREGISTERED for every push token...
// tokens retired, user told on other channels, no silent loss."
// apps/notifier/internal/deliver.sendNativePushToDevice distinguishes this
// from a transient failure using this typed error, retiring the channel
// via DisableChannel instead of retrying it forever.
func TestSend_UnregisteredTokenReturnsTypedError(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(fcmErrorResponse{
			Error: struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}{Status: "UNREGISTERED", Message: "Requested entity was not found."},
		})
	})

	_, err := c.Send(context.Background(), "stale-token", Notification{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var invalidErr *TokenInvalidError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("err = %v, want a *TokenInvalidError", err)
	}
	if invalidErr.Status != "UNREGISTERED" {
		t.Errorf("Status = %q", invalidErr.Status)
	}
}

func TestSend_GenericAPIErrorIsNotTypedInvalid(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(fcmErrorResponse{
			Error: struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}{Status: "INTERNAL", Message: "backend error"},
		})
	})

	_, err := c.Send(context.Background(), "tok", Notification{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var invalidErr *TokenInvalidError
	if errors.As(err, &invalidErr) {
		t.Error("a generic INTERNAL error must not be treated as a permanently invalid token")
	}
}
