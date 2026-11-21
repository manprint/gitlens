package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/manprint/pglens/internal/mcp"
)

// mcpMaxBody caps one JSON-RPC message.
const mcpMaxBody = 1 << 20

const mcpInstructions = "Read-only access to pglens PostgreSQL monitoring data: instances, clusters, " +
	"session activity, statements, plans, locks, logs, autovacuum, host metrics, findings and alerts. " +
	"Start with pglens_list_instances to get the instance ids the other tools take. Times are RFC 3339."

// mcpHandler serves POST /mcp. The credential is the REST one, checked here
// because /mcp is outside /api/; every tool call is then served by inner (the
// whole router) as a GET carrying only the caller's Authorization and Cookie,
// so the REST gate, validation and limits apply unchanged.
func mcpHandler(cfg UIConfig, auth *Auth, store *SessionStore, inner http.Handler) http.Handler {
	srv := &mcp.Server{
		Info:         mcp.Info{Name: "pglens", Version: cfg.MCP.Version},
		Instructions: mcpInstructions,
		Tools:        mcpTools(),
	}
	allowed := map[string]bool{}
	for _, o := range cfg.MCP.AllowedOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "MCP accepts POST only")
			return
		}
		if !credentialAccepted(cfg, auth, store, w, r) {
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !strings.EqualFold(origin, ownOrigin(r)) && !allowed[strings.ToLower(origin)] {
			writeError(w, http.StatusForbidden, "origin_not_allowed", "origin not allowed; list it in PGLENS_MCP_ALLOWED_ORIGINS")
			return
		}
		if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || media != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
			return
		}
		if r.ContentLength > mcpMaxBody {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "message over 1 MiB")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpMaxBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "too_large", "message over 1 MiB")
				return
			}
			writeError(w, http.StatusBadRequest, "bad_request", "cannot read the message")
			return
		}
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !mcp.Supported(v) {
			writeError(w, http.StatusBadRequest, "unsupported_protocol_version", "supported: "+strings.Join(mcp.SupportedVersions(), ", "))
			return
		}
		logMCPCall(body)
		resp, notification := srv.Handle(r.Context(), body, mcpCaller(r, inner))
		if notification {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	})
}

// ownOrigin is the origin a browser sends for a page served by this server.
func ownOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// logMCPCall logs the method and tool name at debug level, never arguments.
func logMCPCall(body []byte) {
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &msg) != nil {
		return
	}
	if msg.Method == "tools/call" {
		slog.Debug("mcp call", "method", msg.Method, "tool", msg.Params.Name)
		return
	}
	slog.Debug("mcp call", "method", msg.Method)
}

// mcpCaller serves a tool call in-process: a GET built from the tool and
// arguments, with only the outer Authorization and Cookie headers.
func mcpCaller(outer *http.Request, inner http.Handler) mcp.Caller {
	return func(ctx context.Context, tool mcp.Tool, args map[string]json.RawMessage) (int, []byte, error) {
		target, err := tool.Target(args)
		if err != nil {
			return 0, nil, err
		}
		// The outer request's chi route context (method POST, path /mcp) must
		// not reach the router, or it routes the GET as that request.
		ctx = context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Host = outer.Host
		req.RemoteAddr = outer.RemoteAddr
		for _, name := range []string{"Authorization", "Cookie"} {
			if values := outer.Header.Values(name); len(values) > 0 {
				req.Header[name] = append([]string(nil), values...)
			}
		}
		req.Header.Set("Accept", "application/json")
		rec := &memoryResponse{header: http.Header{}, limit: mcp.ResultLimit + 1}
		inner.ServeHTTP(rec, req)
		return rec.statusCode(), rec.body, nil
	}
}

// memoryResponse is an http.ResponseWriter that keeps the status and at most
// limit bytes of the body (enough to tell an oversized result).
type memoryResponse struct {
	header http.Header
	status int
	body   []byte
	limit  int
}

func (m *memoryResponse) Header() http.Header { return m.header }

func (m *memoryResponse) WriteHeader(status int) {
	if m.status == 0 {
		m.status = status
	}
}

func (m *memoryResponse) Write(p []byte) (int, error) {
	m.WriteHeader(http.StatusOK)
	if room := m.limit - len(m.body); room > 0 {
		if len(p) > room {
			m.body = append(m.body, p[:room]...)
		} else {
			m.body = append(m.body, p...)
		}
	}
	return len(p), nil
}

func (m *memoryResponse) statusCode() int {
	if m.status == 0 {
		return http.StatusOK
	}
	return m.status
}
