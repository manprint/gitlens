//go:build integration

package server

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
)

// smtpSink is a plain SMTP server on loopback that hands over each message it
// receives.
func smtpSink(t *testing.T) (int, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	messages := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				r := bufio.NewReader(conn)
				reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
				reply("220 sink ESMTP")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.ToUpper(strings.SplitN(strings.TrimSpace(line), " ", 2)[0]) {
					case "DATA":
						reply("354 go on")
						var b strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil || l == ".\r\n" {
								break
							}
							b.WriteString(l)
						}
						messages <- b.String()
						reply("250 queued")
					case "QUIT":
						reply("221 bye")
						return
					default:
						reply("250 ok")
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, messages
}

// INT-NCH-002: recipients set from the console on a server with a plain
// loopback mail server receive a critical alert through the email slot.
func TestINTNCH002_EmailDeliversToLoopbackSMTP(t *testing.T) {
	port, messages := smtpSink(t)
	cfg := AlertConfig{SMTP: SMTPConfig{Host: "127.0.0.1", Port: port, From: "pglens <alerts@canary.example>", TLS: notify.SMTPNoTLS}}
	api, _ := storedAPI(cfg)
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/email", urlBody(t, "ops@canary.example, dba@canary.example"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var slot notify.Channel
	for _, c := range api.NotificationChannels() {
		if c.Name() == "email" {
			slot = c
		}
	}
	require.NotNil(t, slot)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := notify.Message{DedupID: "int-nch-002", Severity: "critical", Phase: "fire", Summary: "replication lag high", Cluster: "7", At: time.Now().UTC()}
	require.NoError(t, slot.Send(ctx, m))

	select {
	case msg := <-messages:
		var subject string
		for _, line := range strings.Split(msg, "\r\n") {
			if strings.HasPrefix(line, "Subject: ") {
				subject = strings.TrimPrefix(line, "Subject: ")
			}
		}
		require.True(t, strings.HasPrefix(subject, "[pglens] CRITICAL"), subject)
		require.Contains(t, msg, "To: <ops@canary.example>, <dba@canary.example>\r\n")
		require.Contains(t, msg, "Message-ID: <int-nch-002."+strconv.FormatInt(m.At.UnixNano(), 10)+"@pglens>")
	case <-ctx.Done():
		t.Fatal("no message reached the SMTP listener")
	}
}
