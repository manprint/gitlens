// Command endpoints serves the two local notification sinks of the plan 007
// reference scenario (test/parity/README.md): a Teams-style webhook that
// appends each request body as one line to a file, and an SMTP sink that
// appends each message to another file. Both bind the given loopback
// addresses only; nothing is forwarded anywhere.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	httpAddr := flag.String("http", "127.0.0.1:18081", "webhook listen address")
	smtpAddr := flag.String("smtp", "127.0.0.1:12525", "SMTP sink listen address")
	webhookFile := flag.String("webhook-file", "webhook.jsonl", "file the webhook bodies are appended to")
	mailFile := flag.String("mail-file", "mail.txt", "file the messages are appended to")
	flag.Parse()

	for _, addr := range []string{*httpAddr, *smtpAddr} {
		if err := loopbackOnly(addr); err != nil {
			log.Fatal(err)
		}
	}
	hooks := &appender{path: *webhookFile}
	mails := &appender{path: *mailFile}

	ln, err := net.Listen("tcp", *smtpAddr)
	if err != nil {
		log.Fatalf("smtp listen: %v", err)
	}
	go serveSMTP(ln, mails)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		var compact bytes.Buffer
		if json.Compact(&compact, body) != nil {
			compact.Reset()
			compact.WriteString(strings.ReplaceAll(string(body), "\n", " "))
		}
		if err := hooks.add(r.URL.Path + "\t" + compact.String() + "\n"); err != nil {
			http.Error(w, "write", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("1"))
	})
	srv := &http.Server{Addr: *httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("webhook on %s, smtp on %s", *httpAddr, *smtpAddr)
	log.Fatal(srv.ListenAndServe())
}

func loopbackOnly(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s: only a loopback address is allowed", addr)
	}
	return nil
}

type appender struct {
	mu   sync.Mutex
	path string
}

func (a *appender) add(text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func serveSMTP(ln net.Listener, mails *appender) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("smtp accept: %v", err)
			return
		}
		go smtpSession(conn, mails)
	}
}

// smtpSession speaks the few SMTP commands a plain (no TLS, no AUTH) client
// sends and records each DATA block.
func smtpSession(conn net.Conn, mails *appender) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	in := bufio.NewReader(conn)
	reply := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }
	reply("220 parity sink")
	var envelope []string
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250 parity")
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
			envelope = append(envelope, strings.TrimSpace(line))
			reply("250 ok")
		case cmd == "DATA":
			reply("354 end with .")
			var msg strings.Builder
			for {
				l, err := in.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
				msg.WriteString(l)
			}
			if err := mails.add("=== " + strings.Join(envelope, " ") + "\n" + msg.String() + "\n"); err != nil {
				reply("451 write failed")
				continue
			}
			envelope = nil
			reply("250 queued")
		case cmd == "RSET", cmd == "NOOP":
			envelope = nil
			reply("250 ok")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}
