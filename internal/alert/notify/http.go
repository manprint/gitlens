package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func sendJSON(ctx context.Context, client *http.Client, url string, body []byte) error {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, attemptCancel := context.WithTimeout(ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
		var resp *http.Response
		if err == nil {
			resp, err = client.Do(req)
		}
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 200))
			_ = resp.Body.Close()
			attemptCancel()
			if readErr != nil {
				err = readErr
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			} else {
				err = fmt.Errorf("notification to [redacted webhook] returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
				if resp.StatusCode < 500 {
					return redactError(err, url)
				}
			}
		} else {
			attemptCancel()
		}
		last = redactError(err, url)
		if attempt < 3 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return err
			}
		}
	}
	return last
}

func redactError(err error, url string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if url != "" {
		msg = strings.ReplaceAll(msg, url, "[redacted webhook]")
	}
	for _, host := range []string{"hooks.slack.com/", "chat.googleapis.com/"} {
		msg = redactHost(msg, host)
	}
	return fmt.Errorf("%s", msg)
}

// redactHost replaces every path following host, up to whitespace, a quote or a
// closing parenthesis, so webhook paths and their key/token query never leak.
func redactHost(msg, host string) string {
	var b strings.Builder
	for {
		i := strings.Index(msg, host)
		if i < 0 {
			b.WriteString(msg)
			return b.String()
		}
		rest := msg[i+len(host):]
		end := strings.IndexAny(rest, " \t\r\n\"')")
		if end < 0 {
			end = len(rest)
		}
		b.WriteString(msg[:i] + host + "[redacted]")
		msg = rest[end:]
	}
}

func sleepBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(attempt*attempt) * time.Second
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
