package emailalert

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

func TestPoller_Enabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"fully configured", Config{Host: "imap.example.com", Username: "u", Password: "p"}, true},
		{"zero value", Config{}, false},
		{"missing password", Config{Host: "imap.example.com", Username: "u"}, false},
		{"missing host", Config{Username: "u", Password: "p"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPoller(c.cfg, nil, slog.Default())
			if got := p.Enabled(); got != c.want {
				t.Errorf("Enabled() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNewPoller_DefaultsMailboxToInbox(t *testing.T) {
	p := NewPoller(Config{Host: "h", Username: "u", Password: "p"}, nil, slog.Default())
	if p.cfg.Mailbox != "INBOX" {
		t.Errorf("Mailbox = %q, want INBOX", p.cfg.Mailbox)
	}
}

func TestExtractTextBody_HTML(t *testing.T) {
	raw := "From: a@b.com\r\nSubject: hi\r\nContent-Type: text/html; charset=utf-8\r\nMIME-Version: 1.0\r\n\r\n<html><body>hello</body></html>"
	got, err := extractTextBody(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extractTextBody: %v", err)
	}
	if got != "<html><body>hello</body></html>" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTextBody_PlainTextFallback(t *testing.T) {
	raw := "From: a@b.com\r\nSubject: hi\r\nContent-Type: text/plain; charset=utf-8\r\nMIME-Version: 1.0\r\n\r\nplain text body"
	got, err := extractTextBody(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extractTextBody: %v", err)
	}
	if got != "plain text body" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTextBody_MultipartPrefersHTML(t *testing.T) {
	raw := "From: a@b.com\r\n" +
		"Subject: hi\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=BOUNDARY\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"plain version\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>html version</p>\r\n" +
		"--BOUNDARY--\r\n"
	got, err := extractTextBody(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extractTextBody: %v", err)
	}
	if got != "<p>html version</p>" {
		t.Errorf("got %q, want the html part chosen over the plain one", got)
	}
}

func TestExtractTextBody_MalformedDoesNotPanic(t *testing.T) {
	_, err := extractTextBody(strings.NewReader("not a mime message at all\x00\xff"))
	// Either a clean error or a degraded empty body is acceptable — the
	// point of this test is that it returns rather than panicking.
	_ = err
}

func TestRedactPassword(t *testing.T) {
	err := redactPassword(errAuthFailed("login rejected: hunter2 invalid"), "hunter2")
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("password leaked into error: %q", err.Error())
	}

	if redactPassword(nil, "hunter2") != nil {
		t.Error("redactPassword(nil, ...) should return nil")
	}
}

type errAuthFailed string

func (e errAuthFailed) Error() string { return string(e) }

// The matched-sender, matched-body path is already covered end-to-end by
// TestIndeed_Glassdoor_Handshake and TestLinkedIn_* against MatchProvider
// and Parse directly, so these two only need to exercise extract's own
// short-circuits: no envelope, and an envelope whose sender no provider
// recognizes.
func TestExtract_UnrecognizedSenderReturnsEmpty(t *testing.T) {
	p := NewPoller(Config{Host: "h", Username: "u", Password: "p"}, nil, slog.Default())
	msg := &imap.Message{Envelope: &imap.Envelope{From: []*imap.Address{{MailboxName: "someone", HostName: "example.com"}}}}
	provider, postings := p.extract(nil, msg)
	if provider != "" || postings != nil {
		t.Errorf("got provider=%q postings=%v, want empty for an unrecognized sender", provider, postings)
	}
}

func TestExtract_NilEnvelopeReturnsEmpty(t *testing.T) {
	p := NewPoller(Config{Host: "h", Username: "u", Password: "p"}, nil, slog.Default())
	provider, postings := p.extract(nil, &imap.Message{})
	if provider != "" || postings != nil {
		t.Errorf("got provider=%q postings=%v, want empty for a nil envelope", provider, postings)
	}
}

func TestPollOnce_ContextCancelledBeforeDial(t *testing.T) {
	p := NewPoller(Config{Host: "127.0.0.1", Port: 1, Username: "u", Password: "p"}, nil, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.pollOnce(ctx); err == nil {
		t.Error("pollOnce with a cancelled context should fail rather than hang")
	}
}
