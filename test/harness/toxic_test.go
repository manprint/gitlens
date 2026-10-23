//go:build e2e

package harness

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	toxiproxyapi "github.com/Shopify/toxiproxy/v2/client"
)

func TestLatency_ToxicAttrs(t *testing.T) {
	l := Latency{Ms: 100, JitterMs: 20}
	if l.toxicType() != "latency" {
		t.Fatalf("type: got %q, want latency", l.toxicType())
	}
	attrs := l.toxicAttrs()
	if attrs["latency"] != int64(100) || attrs["jitter"] != int64(20) {
		t.Fatalf("unexpected attrs: %+v", attrs)
	}
}

func TestTimeout_ToxicAttrs(t *testing.T) {
	tm := Timeout{}
	if tm.toxicType() != "timeout" {
		t.Fatalf("type: got %q, want timeout", tm.toxicType())
	}
	if tm.toxicAttrs()["timeout"] != int64(0) {
		t.Fatalf("expected immediate timeout, got %+v", tm.toxicAttrs())
	}
}

func TestBandwidth_ToxicAttrs(t *testing.T) {
	b := Bandwidth{KBps: 50}
	if b.toxicType() != "bandwidth" {
		t.Fatalf("type: got %q, want bandwidth", b.toxicType())
	}
	if b.toxicAttrs()["rate"] != int64(50) {
		t.Fatalf("unexpected attrs: %+v", b.toxicAttrs())
	}
}

func TestLimitData_ToxicAttrs(t *testing.T) {
	l := LimitData{Bytes: 1024}
	if l.toxicType() != "limit_data" {
		t.Fatalf("type: got %q, want limit_data", l.toxicType())
	}
	if l.toxicAttrs()["bytes"] != int64(1024) {
		t.Fatalf("unexpected attrs: %+v", l.toxicAttrs())
	}
}

func TestResetPeer_ToxicAttrs(t *testing.T) {
	rp := ResetPeer{}
	if rp.toxicType() != "reset_peer" {
		t.Fatalf("type: got %q, want reset_peer", rp.toxicType())
	}
	if len(rp.toxicAttrs()) != 0 {
		t.Fatalf("expected empty attrs, got %+v", rp.toxicAttrs())
	}
}

func TestLinkConstructors(t *testing.T) {
	server := LinkAgentToServer()
	if server.kind != "server" {
		t.Fatalf("LinkAgentToServer: got kind %q", server.kind)
	}
	pg := LinkAgentToPG("pg-primary")
	if pg.kind != "pg" || pg.svc != "pg-primary" {
		t.Fatalf("LinkAgentToPG: got %+v", pg)
	}
}

// toxiproxyEndpoint is the proxy the fake API below serves.
const toxiproxyEndpoint = "/proxies/agent-to-server"

// proxyJSON is the proxy lookup Toxic makes before it posts any toxic.
const proxyJSON = `{"name":"agent-to-server","listen":"0.0.0.0:20000","upstream":"pglens-server:8080","enabled":true}`

// fakeToxiproxyAPI answers the endpoints Toxic and its RemoveFunc use for the
// agent-to-server proxy. A toxic POST for failStream gets a 500, and the names
// of the DELETEd toxics come back from the returned accessor.
func fakeToxiproxyAPI(t *testing.T, failStream string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == toxiproxyEndpoint:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, proxyJSON)
		case r.Method == http.MethodPost && r.URL.Path == toxiproxyEndpoint+"/toxics":
			var body struct {
				Stream string `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.Stream == failStream {
				http.Error(w, "injected failure", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, toxiproxyEndpoint+"/toxics/"):
			mu.Lock()
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, toxiproxyEndpoint+"/toxics/"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), deleted...)
	}
}

// newToxicTestHarness wires a Harness to a fake toxiproxy URL, the same way
// InitToxiproxy does, with the given timeout on the raw HTTP calls.
func newToxicTestHarness(t *testing.T, url string, httpTimeout time.Duration) *Harness {
	t.Helper()
	h := &Harness{}
	setToxicState(h, &ToxicState{
		enabled:      true,
		client:       toxiproxyapi.NewClient(url),
		toxiproxyURL: url,
		proxyNames:   map[string]string{LinkAgentToServer().kind: "agent-to-server"},
		httpClient:   &http.Client{Timeout: httpTimeout},
	})
	t.Cleanup(func() { toxicRegistry.Delete(h) })
	return h
}

// TestToxic_RemoveDeletesBothStreams proves the RemoveFunc of a successful
// Toxic call removes the upstream and the downstream toxic.
func TestToxic_RemoveDeletesBothStreams(t *testing.T) {
	srv, deleted := fakeToxiproxyAPI(t, "")
	h := newToxicTestHarness(t, srv.URL, 5*time.Second)

	remove, err := h.Toxic(LinkAgentToServer(), Latency{Ms: 100})
	if err != nil {
		t.Fatalf("Toxic: %v", err)
	}
	if err := remove(); err != nil {
		t.Fatalf("remove: %v", err)
	}
	want := []string{"toxic-1-latency-upstream", "toxic-1-latency-downstream"}
	if got := deleted(); !reflect.DeepEqual(got, want) {
		t.Errorf("deleted toxics: got %v, want %v", got, want)
	}
}

// TestToxic_DownstreamFailureRemovesUpstream proves a Toxic call that fails
// on its second stream does not leave the first toxic on the shared proxy,
// because the caller never receives a RemoveFunc for it.
func TestToxic_DownstreamFailureRemovesUpstream(t *testing.T) {
	srv, deleted := fakeToxiproxyAPI(t, "downstream")
	h := newToxicTestHarness(t, srv.URL, 5*time.Second)

	_, err := h.Toxic(LinkAgentToServer(), Latency{Ms: 100})
	if err == nil || !strings.Contains(err.Error(), "downstream") {
		t.Fatalf("Toxic error: got %v, want the downstream creation failure", err)
	}
	if want := []string{"toxic-1-latency-upstream"}; !reflect.DeepEqual(deleted(), want) {
		t.Errorf("cleanup deleted %v, want %v", deleted(), want)
	}
}

// TestToxic_StalledAPIFailsAtTheClientTimeout proves a toxiproxy API that
// never answers a toxic POST fails the call at the client timeout instead of
// blocking the test.
func TestToxic_StalledAPIFailsAtTheClientTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == toxiproxyEndpoint {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, proxyJSON)
			return
		}
		<-release // never answer the toxic POST
	}))
	defer srv.Close()
	defer close(release) // runs before srv.Close, so the stalled handler can return
	h := newToxicTestHarness(t, srv.URL, 200*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := h.Toxic(LinkAgentToServer(), Latency{Ms: 100})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Toxic succeeded against a stalled API")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Toxic still blocked 10s after a 200ms client timeout")
	}
}
