// Package telegram implements the notifier's Telegram Bot API client —
// docs/11-notifications.md section 5 ("Telegram... needs 60 seconds of
// setup, and reaches every platform") and section 11's channel setup flow.
//
// Long-poll (getUpdates), not a webhook: docs/15-infrastructure-deployment.md
// section 8 and ADR-014 both point the same way — a webhook needs a public
// endpoint, and Scout has none (Tailscale only). getUpdates needs nothing
// but an outbound connection this process already makes.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPIBase = "https://api.telegram.org"

// requestTimeout bounds sendMessage.
const requestTimeout = 15 * time.Second

// Client is a minimal Telegram Bot API client — sendMessage and getUpdates
// only, which is all the notifier needs. No webhook management, no
// keyboard/inline-button callback handling (P1 delivers text messages;
// docs/11 section 6.1's inline buttons are a later milestone).
//
// Two http.Client values, not one: net/http's Client.Timeout is an
// absolute wall-clock bound on the whole request, independent of and
// enforced ahead of any context deadline — found live during setup, where
// a 30s long-poll GetUpdates call failed every time with "Client.Timeout
// exceeded while awaiting headers" because it shared sendMessage's fixed
// 15s client. GetUpdates' client has no fixed Timeout and relies entirely
// on the context deadline it constructs per call instead.
type Client struct {
	token        string
	apiBase      string
	http         *http.Client
	longPollHTTP *http.Client
}

// New returns a Client. An empty token makes every method return an error
// immediately rather than attempting a request that could only fail —
// mirrors apps/collector/internal/heartbeat.Pinger's Enabled() pattern for
// "not configured" being a valid, quiet state in local development.
func New(token string) *Client {
	return &Client{
		token:        token,
		apiBase:      defaultAPIBase,
		http:         &http.Client{Timeout: requestTimeout},
		longPollHTTP: &http.Client{}, // no fixed Timeout; see the type comment
	}
}

// NewWithBaseURL is New with the API base URL overridden, so tests —
// including apps/notifier/internal/deliver's, in a different package — can
// point a Client at an httptest.Server instead of the real Telegram API.
func NewWithBaseURL(token, baseURL string) *Client {
	c := New(token)
	c.apiBase = baseURL
	return c
}

// Enabled reports whether a bot token was configured.
func (c *Client) Enabled() bool { return c.token != "" }

// Update is the subset of Telegram's Update object the notifier reads.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message is the subset of Telegram's Message object the notifier reads.
type Message struct {
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

// SendMessage delivers text to chatID. Returns the provider message ID
// (Telegram's own message_id, stringified) for
// notification_delivery.provider_msg_id.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("telegram: not configured (no bot token)")
	}

	form := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {text},
		"parse_mode": {"Markdown"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/bot"+c.token+"/sendMessage", nil)
	if err != nil {
		return "", fmt.Errorf("telegram: build request: %w", err)
	}
	req.URL.RawQuery = form.Encode()

	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.do(c.http, req, &result); err != nil {
		return "", err
	}
	return strconv.FormatInt(result.MessageID, 10), nil
}

// GetUpdates long-polls for new messages sent to the bot — used only by the
// one-time onboarding flow (apps/notifier/cmd's "-setup" mode), not by the
// steady-state notifier. offset is the update_id to start after (0 for
// "everything currently queued"); timeoutSeconds is Telegram's own
// long-poll hold time, bounded by this call's own context deadline at
// timeoutSeconds+5s — see the Client type comment for why this uses a
// separate http.Client with no fixed Timeout rather than c.http.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("telegram: not configured (no bot token)")
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds+5)*time.Second)
	defer cancel()

	form := url.Values{
		"offset":  {strconv.FormatInt(offset, 10)},
		"timeout": {strconv.Itoa(timeoutSeconds)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBase+"/bot"+c.token+"/getUpdates", nil)
	if err != nil {
		return nil, fmt.Errorf("telegram: build request: %w", err)
	}
	req.URL.RawQuery = form.Encode()

	var updates []Update
	if err := c.do(c.longPollHTTP, req, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// do sends req via the given client and decodes Telegram's
// {ok, result, description} envelope into out. The bot token lives only in
// the URL path this method builds internally — never logged, matching
// AGENTS.md rule 7 and the pattern apps/collector/internal/heartbeat uses
// for its own outbound secrets.
func (c *Client) do(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: request failed: %w", redactToken(err, c.token))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telegram: read response: %w", err)
	}

	var envelope apiResponse[json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("telegram: decode response envelope: %w", err)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram: api error: %s", envelope.Description)
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("telegram: decode result: %w", err)
	}
	return nil
}

// redactToken strips the bot token out of a wrapped *url.Error's message —
// net/http embeds the full request URL (including the token) in transport
// errors, and this is the one path in this package where that could
// otherwise leak into a log line.
func redactToken(err error, token string) error {
	if token == "" || err == nil {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
}
