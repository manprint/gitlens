package notify

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeSMTP is a plain SMTP server on loopback that records the commands and
// the message it receives.
type fakeSMTP struct {
	addr     string
	mu       sync.Mutex
	commands []string
	data     string
}

func startFakeSMTP(t *testing.T, extensions ...string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, extensions)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn, extensions []string) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		f.mu.Lock()
		f.commands = append(f.commands, verb)
		f.mu.Unlock()
		switch verb {
		case "EHLO":
			for _, ext := range extensions {
				reply("250-" + ext)
			}
			reply("250 fake")
		case "AUTH":
			reply("235 ok")
		case "DATA":
			reply("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (f *fakeSMTP) seen() ([]string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...), f.data
}

// dialTo sends every connection of the test to addr and counts them.
func dialTo(t *testing.T, addr string) *int {
	t.Helper()
	calls := new(int)
	previous := smtpDial
	smtpDial = func(ctx context.Context, _ string) (net.Conn, error) {
		*calls++
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	t.Cleanup(func() { smtpDial = previous })
	return calls
}

var testSMTP = SMTPSettings{Host: "127.0.0.1", Port: 25, From: "pglens <alerts@canary.example>", TLS: SMTPNoTLS}

// NCH-020: the message is the expected header block and plain body, CRLF
// line ends throughout.
func TestNCH020_EmailGolden(t *testing.T) {
	m := Message{
		DedupID: "dedup-1", RuleID: "replication_lag", Severity: "critical", Phase: "fire",
		Summary: "replication lag high", Cluster: "7", Instance: "i1", Database: "odoo", Value: 42.5,
		At: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
	}
	got := string(buildEmail(testSMTP, []string{"ops@canary.example", "Dba <dba@canary.example>"}, m))
	want := "From: \"pglens\" <alerts@canary.example>\r\n" +
		"To: <ops@canary.example>, \"Dba\" <dba@canary.example>\r\n" +
		"Subject: [pglens] CRITICAL replication lag high\r\n" +
		"Date: Thu, 29 Oct 2026 12:00:00 +0000\r\n" +
		"Message-ID: <dedup-1.1793275200000000000@pglens>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"replication lag high\r\n" +
		"\r\n" +
		"cluster=7 instance=i1 database=odoo value=42.5 at=2026-10-29T12:00:00Z\r\n"
	require.Equal(t, want, got)
	require.NotContains(t, strings.ReplaceAll(got, "\r\n", ""), "\n")

	m.Detail = &Detail{Title: "Lag", Fields: []Field{{Label: "Server", Value: "i1"}}, Sections: []Section{{Title: "Check", Code: []string{"SELECT 1;"}}}, Footer: "pglens"}
	m.Summary = "é" + strings.Repeat("x", 2000)
	got = string(buildEmail(testSMTP, []string{"ops@canary.example"}, m))
	body := got[strings.Index(got, "\r\n\r\n")+4:]
	require.Equal(t, "Lag\r\n\r\nServer: i1\r\n\r\nCheck\r\nSELECT 1;\r\n\r\npglens\r\n", body)
	require.Contains(t, got, "Subject: =?utf-8?q?")
	for _, line := range strings.Split(got, "\r\n") {
		require.LessOrEqual(t, len(line), 998)
	}
	require.Equal(t, []string{strings.Repeat("é", 499), "x"}, splitLine(strings.Repeat("é", 499)+"x", 998))
	require.Equal(t, "email", NewEmail(testSMTP, nil).Name())
}

// NCH-025: a resolve message says RESOLVED in the subject, not the severity
// of the alert it closes.
func TestNCH025_EmailResolvedSubject(t *testing.T) {
	m := Message{DedupID: "d", Severity: "critical", Phase: "resolve", Summary: "replication lag high", At: time.Unix(0, 0).UTC()}
	got := string(buildEmail(testSMTP, []string{"ops@canary.example"}, m))
	require.Contains(t, got, "Subject: [pglens] RESOLVED replication lag high\r\n")
	m.Phase = "fire"
	require.Contains(t, string(buildEmail(testSMTP, []string{"ops@canary.example"}, m)), "Subject: [pglens] CRITICAL replication lag high\r\n")
}

// NCH-021: a summary carrying CRLF cannot add a header.
func TestNCH021_EmailHeaderInjection(t *testing.T) {
	m := Message{Severity: "warning", Summary: "lag\r\nBcc: x@y", At: time.Unix(0, 0).UTC()}
	got := string(buildEmail(testSMTP, []string{"ops@canary.example\r\nBcc: z@y"}, m))
	head := got[:strings.Index(got, "\r\n\r\n")]
	var names []string
	for _, line := range strings.Split(head, "\r\n") {
		names = append(names, strings.SplitN(line, ":", 2)[0])
	}
	require.Equal(t, []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"}, names)
	require.Contains(t, head, "Subject: [pglens] WARNING lagBcc: x@y")
}

// NCH-022: starttls fails against a server without STARTTLS; none with a
// user name to a remote host fails before connecting, so before AUTH.
func TestNCH022_EmailTransportRefusals(t *testing.T) {
	srv := startFakeSMTP(t, "AUTH PLAIN")
	dials := dialTo(t, srv.addr)
	m := Message{Severity: "critical", Summary: "s"}

	cfg := testSMTP
	cfg.TLS = SMTPStartTLS
	err := NewEmail(cfg, []string{"ops@canary.example"}).Send(context.Background(), m)
	require.ErrorContains(t, err, "does not offer STARTTLS")
	commands, _ := srv.seen()
	require.NotContains(t, commands, "AUTH")
	require.NotContains(t, commands, "MAIL")

	cfg = testSMTP
	cfg.Host, cfg.Username, cfg.Password = "mail.canary.example", "user", "s3cretPassw0rd"
	before := *dials
	err = NewEmail(cfg, []string{"ops@canary.example"}).Send(context.Background(), m)
	require.ErrorContains(t, err, "only over TLS")
	require.Equal(t, before, *dials)
	require.NotContains(t, err.Error(), "s3cretPassw0rd")
}

// NCH-022 (delivery): plain to loopback delivers the message; a refused
// recipient's error does not quote it.
func TestNCH022_EmailPlainLoopbackDelivers(t *testing.T) {
	srv := startFakeSMTP(t, "AUTH PLAIN")
	dialTo(t, srv.addr)
	cfg := testSMTP
	cfg.Username, cfg.Password = "user", "s3cretPassw0rd"
	err := NewEmail(cfg, []string{"ops@canary.example"}).Send(context.Background(), Message{Severity: "critical", Summary: "s", DedupID: "d"})
	require.NoError(t, err)
	commands, data := srv.seen()
	require.Equal(t, []string{"EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT"}, commands)
	require.True(t, strings.HasPrefix(data[strings.Index(data, "Subject: "):], "Subject: [pglens] CRITICAL s\r\n"))

	e := &email{cfg: cfg, recipients: []string{"Ops <ops@canary.example>"}}
	require.NotContains(t, e.redact(errorString("550 <ops@canary.example> unknown; pass s3cretPassw0rd")).Error(), "ops@canary.example")
	require.NotContains(t, e.redact(errorString("x s3cretPassw0rd")).Error(), "s3cretPassw0rd")
}

type errorString string

func (e errorString) Error() string { return string(e) }
