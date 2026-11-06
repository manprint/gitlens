package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type statusNode struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Healthy    bool   `json:"healthy"`
	Isolated   bool   `json:"isolated"`
	Upstream   string `json:"upstream"`
	Timeline   int    `json:"timeline"`
	LagBytes   *int64 `json:"lag_bytes"`
	LSN        string `json:"lsn"`
	ReceiveLSN string `json:"receive_lsn"`
	Error      string `json:"error,omitempty"`
}

type statusDoc struct {
	Leader         string          `json:"leader"`
	Timeline       int             `json:"timeline"`
	MaxTimeline    int             `json:"max_timeline"`
	WritesBlocked  bool            `json:"writes_blocked"`
	FailoverPaused bool            `json:"failover_paused"`
	Promoting      *promotingState `json:"promoting"`
	SplitBrain     bool            `json:"split_brain"`
	Since          *time.Time      `json:"since"`
	Failover       string          `json:"failover"`
	LastError      string          `json:"last_error"`
	Nodes          []statusNode    `json:"nodes"`
}

func (c *controller) status() statusDoc {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := statusDoc{
		Leader:         c.leader,
		MaxTimeline:    c.st.MaxTimeline,
		WritesBlocked:  c.st.WritesBlocked,
		FailoverPaused: c.st.FailoverPaused,
		Promoting:      c.st.Promoting,
		SplitBrain:     c.split,
		Failover:       c.failover,
		LastError:      c.lastErr,
		Nodes:          []statusNode{},
	}
	if !c.since.IsZero() {
		s := c.since.UTC()
		d.Since = &s
	}
	var leaderLSN uint64
	for _, v := range c.views {
		if v.Name == c.leader {
			d.Timeline = v.Timeline
			leaderLSN = v.LSN
		}
	}
	for _, v := range c.views {
		n := statusNode{Name: v.Name, Healthy: v.Reachable, Isolated: v.Isolated, Error: v.Err}
		switch {
		case !v.Reachable:
			n.Role = "unreachable"
		case v.InRecovery:
			n.Role = "standby"
			n.Upstream = v.Upstream
			lag := int64(0)
			if c.leader != "" && leaderLSN > v.LSN {
				lag = int64(leaderLSN - v.LSN)
			}
			n.LagBytes = &lag
		case v.Name == c.leader || (c.split && v.Timeline == c.st.MaxTimeline):
			n.Role = "primary"
		default:
			n.Role = "diverged_or_stale"
		}
		if v.Reachable {
			n.Timeline = v.Timeline
			n.LSN = formatLSN(v.LSN)
			n.ReceiveLSN = formatLSN(v.ReceiveLSN)
		}
		d.Nodes = append(d.Nodes, n)
	}
	return d
}

func (c *controller) handler(ctx context.Context) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			code := http.StatusConflict
			if errors.Is(err, context.DeadlineExceeded) {
				code = http.StatusGatewayTimeout
			}
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(c.status())
	}
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) { reply(w, nil) })
	mux.HandleFunc("POST /block-writes", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.setWritesBlocked(true)) })
	mux.HandleFunc("POST /unblock-writes", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.setWritesBlocked(false)) })
	mux.HandleFunc("POST /pause-failover", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.setFailoverPaused(true)) })
	mux.HandleFunc("POST /resume-failover", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.setFailoverPaused(false)) })
	mux.HandleFunc("POST /clear-promoting", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.clearPromoting()) })
	mux.HandleFunc("POST /fault/isolate", func(w http.ResponseWriter, r *http.Request) {
		reply(w, c.setIsolated(r.URL.Query().Get("node"), true))
	})
	mux.HandleFunc("POST /fault/clear", func(w http.ResponseWriter, _ *http.Request) { reply(w, c.setIsolated("", false)) })
	mux.HandleFunc("POST /switchover", func(w http.ResponseWriter, r *http.Request) {
		var lsn uint64
		if s := r.URL.Query().Get("lsn"); s != "" {
			v, err := parseLSN(s)
			if err != nil {
				reply(w, err)
				return
			}
			lsn = v
		}
		sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		reply(w, c.switchover(sctx, r.URL.Query().Get("target"), lsn, 30*time.Second))
	})
	return mux
}
