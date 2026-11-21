package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The TLS modes of an SMTP server.
const (
	SMTPStartTLS = "starttls"
	SMTPTLS      = "tls"
	SMTPNoTLS    = "none"
)

// SMTPSettings is the mail server the email channel sends through.
type SMTPSettings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// TLS is SMTPStartTLS, SMTPTLS or SMTPNoTLS.
	TLS string
}

const (
	emailSubjectMax = 120
	emailLineMax    = 998
	smtpDialTimeout = 10 * time.Second
	// smtpSendTimeout bounds a whole send when the context has no deadline.
	smtpSendTimeout = 45 * time.Second
)

// smtpDial opens the connection to the mail server; tests replace it.
var smtpDial = func(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: smtpDialTimeout}
	return d.DialContext(ctx, "tcp", addr)
}

type email struct {
	cfg        SMTPSettings
	recipients []string
}

// NewEmail returns a channel that mails each message to recipients through
// the SMTP server of cfg.
func NewEmail(cfg SMTPSettings, recipients []string) Channel {
	return &email{cfg: cfg, recipients: recipients}
}

func (e *email) Name() string { return "email" }

func (e *email) Send(ctx context.Context, m Message) error {
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	return e.redact(e.send(ctx, m))
}

// redact removes the recipients and the password from a delivery error: a
// server answer may quote a recipient (I-7).
func (e *email) redact(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	for _, r := range e.recipients {
		if a, perr := mail.ParseAddress(r); perr == nil && a.Address != "" {
			text = strings.ReplaceAll(text, a.Address, "[redacted recipient]")
		}
		if r = strings.TrimSpace(r); r != "" {
			text = strings.ReplaceAll(text, r, "[redacted recipient]")
		}
	}
	if e.cfg.Password != "" {
		text = strings.ReplaceAll(text, e.cfg.Password, "[redacted]")
	}
	return errors.New(text)
}

func (e *email) send(ctx context.Context, m Message) error {
	from, err := mail.ParseAddress(headerValue(e.cfg.From))
	if err != nil {
		return errors.New("smtp: the sender address does not parse")
	}
	rcpts := make([]string, 0, len(e.recipients))
	for _, r := range e.recipients {
		a, err := mail.ParseAddress(headerValue(r))
		if err != nil {
			return errors.New("smtp: a recipient address does not parse")
		}
		rcpts = append(rcpts, a.Address)
	}
	if len(rcpts) == 0 {
		return errors.New("smtp: no recipients")
	}
	if e.cfg.TLS == SMTPNoTLS && e.cfg.Username != "" && !smtpLoopback(e.cfg.Host) {
		return errors.New("smtp: credentials are sent only over TLS, or in plain to localhost")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, smtpSendTimeout)
		defer cancel()
	}
	conn, err := smtpDial(ctx, net.JoinHostPort(e.cfg.Host, strconv.Itoa(e.cfg.Port)))
	if err != nil {
		return fmt.Errorf("smtp: connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tlsConfig := &tls.Config{ServerName: e.cfg.Host, MinVersion: tls.VersionTLS12}
	if e.cfg.TLS == SMTPTLS {
		tc := tls.Client(conn, tlsConfig)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("smtp: tls: %w", err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp: greeting: %w", err)
	}
	defer func() { _ = c.Close() }()
	if e.cfg.TLS == SMTPStartTLS || e.cfg.TLS == "" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: the server does not offer STARTTLS")
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if e.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp: mail from: %w", err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("smtp: rcpt: %w", err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write(buildEmail(e.cfg, e.recipients, m)); err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	return c.Quit()
}

func smtpLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1"
}

// headerValue removes CR and LF, so a value cannot start a header of its own.
func headerValue(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// buildEmail is the message of m as sent after DATA: the header block, a
// blank line and the plain-text body, with CRLF line ends.
func buildEmail(cfg SMTPSettings, recipients []string, m Message) []byte {
	to := make([]string, 0, len(recipients))
	for _, r := range recipients {
		if a, err := mail.ParseAddress(headerValue(r)); err == nil {
			to = append(to, a.String())
		}
	}
	from := headerValue(cfg.From)
	if a, err := mail.ParseAddress(from); err == nil {
		from = a.String()
	}
	subject := headerValue("[pglens] " + strings.ToUpper(m.Severity) + " " + clip(m.Summary, emailSubjectMax))
	var b strings.Builder
	header := func(name, value string) { b.WriteString(name + ": " + headerValue(value) + "\r\n") }
	header("From", from)
	header("To", strings.Join(to, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", m.At.Format(time.RFC1123Z))
	header("Message-ID", "<"+messageIDPart(m.DedupID)+"."+strconv.FormatInt(m.At.UnixNano(), 10)+"@pglens>")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=UTF-8")
	header("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	for _, line := range emailBodyLines(m) {
		for _, part := range splitLine(line, emailLineMax) {
			b.WriteString(part + "\r\n")
		}
	}
	return []byte(b.String())
}

// messageIDPart keeps the letters, digits, dot, dash and underscore of id.
func messageIDPart(id string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, id)
	if out == "" {
		return "message"
	}
	return out
}

// emailBodyLines is the plain text of the Slack message: the summary and the
// context line, or the detail's title, fields, sections and footer.
func emailBodyLines(m Message) []string {
	var lines []string
	if d := m.Detail; d != nil && d.Title != "" {
		lines = append(lines, d.Title, "")
		for _, f := range d.Fields {
			lines = append(lines, f.Label+": "+f.Value)
		}
		if text := teamsDetailText(d); text != "" {
			if len(d.Fields) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, text)
		}
	} else {
		lines = append(lines, m.Summary)
		if !isTest(m) {
			lines = append(lines, "", fmt.Sprintf("cluster=%s instance=%s database=%s value=%v at=%s", m.Cluster, m.Instance, m.Database, m.Value, m.At.UTC().Format(time.RFC3339)))
		}
	}
	var out []string
	for _, l := range lines {
		out = append(out, strings.Split(strings.ReplaceAll(l, "\r", ""), "\n")...)
	}
	return out
}

// splitLine cuts line into parts of at most max bytes, at rune boundaries.
func splitLine(line string, max int) []string {
	var parts []string
	for len(line) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		parts = append(parts, line[:cut])
		line = line[cut:]
	}
	return append(parts, line)
}
