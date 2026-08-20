// IMAP polling — docs/05-source-catalog.md's "Email alert ingestion, in
// detail" and ADR-014's Tailscale-only constraint: a webhook needs a public
// endpoint Scout doesn't have, so this polls INBOX for UNSEEN mail instead
// of waiting for one to be pushed in. IMAP's own \Seen flag is the
// checkpoint — fetching a message's body without Peek implicitly marks it
// seen, so there is no separate cursor to persist or lose.
package emailalert

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	// Registers message.CharsetReader for non-UTF-8 alert mail. Adds ~1MiB
	// to the binary; accepted the same way go-imap/go-message themselves
	// were — see go.mod's justification comment for this dependency pair.
	_ "github.com/emersion/go-message/charset"
)

// DefaultInterval mirrors docs/06's other long-poll-style sources: frequent
// enough that a new alert shows up the same day, infrequent enough that an
// IMAP provider's rate limits are a non-issue at this volume.
const DefaultInterval = 15 * time.Minute

const dialTimeout = 20 * time.Second

// Config is the IMAP account this poller reads from. All fields empty is a
// valid, "not configured" value — see Poller.Enabled.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	// Mailbox defaults to INBOX when empty (NewPoller).
	Mailbox string
}

// Handler is called once per posting recovered from a matched alert email.
// The scheduler package supplies Scheduler.IngestEmailAlert as this —
// Poller itself has no database access, keeping IMAP protocol handling and
// the write-path pipeline in separate packages with a one-directional
// import (scheduler -> emailalert, never the reverse).
type Handler func(ctx context.Context, provider string, posting ExtractedPosting) error

// Poller periodically logs into an IMAP account, searches for UNSEEN mail,
// and hands every posting extracted from a matched provider to Handler.
type Poller struct {
	cfg     Config
	handler Handler
	log     *slog.Logger
}

// NewPoller returns a Poller for cfg. handler must be non-nil if Run is
// ever called on an Enabled Poller.
func NewPoller(cfg Config, handler Handler, log *slog.Logger) *Poller {
	if cfg.Mailbox == "" {
		cfg.Mailbox = "INBOX"
	}
	return &Poller{cfg: cfg, handler: handler, log: log}
}

// Enabled reports whether the account fields needed to log in were all
// configured — mirrors apps/collector/internal/heartbeat.Pinger.Enabled and
// apps/notifier/internal/telegram.Client.Enabled: "not configured" is a
// valid, quiet state, not an error.
func (p *Poller) Enabled() bool {
	return p.cfg.Host != "" && p.cfg.Username != "" && p.cfg.Password != ""
}

// Run polls on a ticker until ctx is cancelled. A disabled Poller returns
// immediately rather than blocking forever on nothing — matches how the
// rest of the collector's optional integrations behave when unconfigured.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	if !p.Enabled() {
		p.log.Warn("email-alert ingestion disabled: SCOUT_EMAIL_IMAP_HOST/USER/PASSWORD not fully set")
		return
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	p.log.Info("email-alert ingestion armed", "host", p.cfg.Host, "mailbox", p.cfg.Mailbox, "interval", interval.String())

	p.poll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	matched, unmatched, err := p.pollOnce(ctx)
	if err != nil {
		p.log.Warn("email-alert poll failed", "err", err)
		return
	}
	if matched+unmatched > 0 {
		p.log.Info("email-alert poll complete", "matched", matched, "unmatched", unmatched)
	}
}

// pollOnce logs in, fetches every UNSEEN message, and hands each matched
// posting to p.handler. A message from a sender no provider recognizes
// still counts toward unmatched but is otherwise ignored — it is still
// marked \Seen by the fetch, same as a matched one, since re-checking mail
// this poller will never understand on every future cycle serves nobody.
func (p *Poller) pollOnce(ctx context.Context) (matched, unmatched int, err error) {
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	dialer := &tls.Dialer{Config: &tls.Config{ServerName: p.cfg.Host, MinVersion: tls.VersionTLS12}}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return 0, 0, fmt.Errorf("dial: %w", err)
	}
	c, err := client.New(conn)
	if err != nil {
		return 0, 0, fmt.Errorf("new client: %w", err)
	}
	defer func() { _ = c.Logout() }()

	if err = c.Login(p.cfg.Username, p.cfg.Password); err != nil {
		return 0, 0, fmt.Errorf("login: %w", redactPassword(err, p.cfg.Password))
	}

	if _, err = c.Select(p.cfg.Mailbox, false); err != nil {
		return 0, 0, fmt.Errorf("select mailbox %q: %w", p.cfg.Mailbox, err)
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	uids, err := c.UidSearch(criteria)
	if err != nil {
		return 0, 0, fmt.Errorf("search unseen: %w", err)
	}
	if len(uids) == 0 {
		return 0, 0, nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	// The zero-value BodySectionName requests the entire message
	// (header+body) with Peek left false, so this FETCH itself sets \Seen
	// — the checkpoint this package relies on instead of a persisted
	// cursor.
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}

	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(seqset, items, messages) }()

	for msg := range messages {
		provider, postings := p.extract(section, msg)
		if provider == "" {
			unmatched++
			continue
		}
		for _, posting := range postings {
			if err := p.handler(ctx, provider, posting); err != nil {
				p.log.Warn("email-alert: ingest posting failed", "provider", provider, "err", err)
				continue
			}
			matched++
		}
	}

	if err := <-done; err != nil {
		return matched, unmatched, fmt.Errorf("fetch: %w", err)
	}
	return matched, unmatched, nil
}

// extract turns one IMAP message into a provider name and its postings.
// Returns ("", nil) for a message with no From header, an unrecognized
// sender, or a body this package's parsers found nothing in — all three
// are the same "not our problem" outcome to the caller.
func (p *Poller) extract(section *imap.BodySectionName, msg *imap.Message) (provider string, postings []ExtractedPosting) {
	if msg.Envelope == nil || len(msg.Envelope.From) == 0 {
		return "", nil
	}
	from := msg.Envelope.From[0].Address()
	prov, ok := MatchProvider(from)
	if !ok {
		return "", nil
	}

	body := msg.GetBody(section)
	if body == nil {
		return "", nil
	}
	htmlBody, err := extractTextBody(body)
	if err != nil {
		p.log.Warn("email-alert: extract body failed", "provider", prov.Name(), "err", err)
		return "", nil
	}

	found, err := prov.Parse(Message{From: from, Subject: msg.Envelope.Subject, HTML: htmlBody, ReceivedAt: msg.Envelope.Date})
	if err != nil {
		p.log.Warn("email-alert: parse failed", "provider", prov.Name(), "err", err)
		return "", nil
	}
	return prov.Name(), found
}

// extractTextBody walks a MIME message for its first text/html part,
// falling back to the first text/plain part if no HTML part exists — every
// provider config in this package is calibrated against HTML alert
// templates, but degrading to plain text is better than discarding a
// message this poller already marked \Seen.
func extractTextBody(r io.Reader) (string, error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		return "", fmt.Errorf("parse mime: %w", err)
	}

	var htmlPart, textPart string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read mime part: %w", err)
		}
		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		raw, err := io.ReadAll(part.Body)
		if err != nil {
			continue
		}
		ct := inline.Get("Content-Type")
		switch {
		case htmlPart == "" && strings.Contains(ct, "text/html"):
			htmlPart = string(raw)
		case textPart == "" && strings.Contains(ct, "text/plain"):
			textPart = string(raw)
		}
	}
	if htmlPart != "" {
		return htmlPart, nil
	}
	return textPart, nil
}

// redactPassword strips the account password out of a login error — go-imap
// wraps the raw server response, which for some providers echoes back
// enough of the failed AUTH command to matter. AGENTS.md rule 7 treats a
// password the same as any other credential that must never reach a log.
func redactPassword(err error, password string) error {
	if err == nil || password == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), password, "[REDACTED]"))
}
