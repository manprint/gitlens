//go:build e2e

package harness

import "testing"

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
