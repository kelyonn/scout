package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetUpdates_NotBoundByShortClientTimeout is a regression test for a
// real failure found live during onboarding: GetUpdates' 30s long-poll
// call failed every time with "Client.Timeout exceeded while awaiting
// headers" because it shared SendMessage's http.Client, whose fixed 15s
// Timeout is an absolute wall-clock bound net/http enforces independently
// of — and ahead of — any context deadline. The fix is a second client
// with no fixed Timeout for GetUpdates specifically; this asserts that
// invariant directly rather than sleeping past 15s in a test.
func TestGetUpdates_NotBoundByShortClientTimeout(t *testing.T) {
	c := New("test-token")
	if c.http.Timeout != requestTimeout {
		t.Errorf("c.http.Timeout = %v, want %v (SendMessage should stay bounded)", c.http.Timeout, requestTimeout)
	}
	if c.longPollHTTP.Timeout != 0 {
		t.Errorf("c.longPollHTTP.Timeout = %v, want 0 (GetUpdates must rely on its own context deadline, not a client-level bound)", c.longPollHTTP.Timeout)
	}
}

func TestSendMessage(t *testing.T) {
	var gotPath string
	var gotChatID, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotChatID = r.URL.Query().Get("chat_id")
		gotText = r.URL.Query().Get("text")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 42},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-token", srv.URL)
	msgID, err := c.SendMessage(context.Background(), 12345, "hello")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msgID != "42" {
		t.Errorf("msgID = %q, want 42", msgID)
	}
	if !strings.Contains(gotPath, "/bottest-token/sendMessage") {
		t.Errorf("path = %q, want it to contain /bottest-token/sendMessage", gotPath)
	}
	if gotChatID != "12345" {
		t.Errorf("chat_id = %q, want 12345", gotChatID)
	}
	if gotText != "hello" {
		t.Errorf("text = %q, want hello", gotText)
	}
}

func TestSendMessage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "description": "chat not found",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-token", srv.URL)
	if _, err := c.SendMessage(context.Background(), 1, "hi"); err == nil {
		t.Fatal("expected an error for a not-ok API response")
	}
}

func TestSendMessage_NotConfigured(t *testing.T) {
	c := New("")
	if _, err := c.SendMessage(context.Background(), 1, "hi"); err == nil {
		t.Fatal("expected an error when no bot token is configured")
	}
}

func TestGetUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{
				{"update_id": 1, "message": map[string]any{"chat": map[string]any{"id": 999}, "text": "hi"}},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-token", srv.URL)
	updates, err := c.GetUpdates(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(updates))
	}
	if updates[0].Message.Chat.ID != 999 {
		t.Errorf("chat id = %d, want 999", updates[0].Message.Chat.ID)
	}
}
