// Command hactl is the lab-grade HA controller of the pglens Odoo stack
// (plan 005, phase 7): health checks, automatic promotion, a read-write and
// a read-only TCP endpoint, and an admin/status HTTP port. Not production.
//
// Environment: HACTL_NODES (name=addr=slot,...), HACTL_PG_USER,
// HACTL_PG_PASSWORD, HACTL_PG_DB, HACTL_REPL_USER, HACTL_REPL_PASSWORD,
// HACTL_STATE, HACTL_RW, HACTL_RO, HACTL_ADMIN.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseNodes(s string) ([]nodeConfig, error) {
	var out []nodeConfig
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		f := strings.Split(item, "=")
		if len(f) != 3 || f[0] == "" || f[1] == "" || f[2] == "" {
			return nil, fmt.Errorf("HACTL_NODES item %q: want name=addr=slot", item)
		}
		out = append(out, nodeConfig{Name: f[0], Addr: f[1], Slot: f[2]})
	}
	if len(out) < 2 {
		return nil, errors.New("HACTL_NODES needs at least two nodes")
	}
	return out, nil
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func healthcheck() int {
	addr := env("HACTL_ADMIN", ":8008")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + addr + "/status")
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run() error {
	nodes, err := parseNodes(os.Getenv("HACTL_NODES"))
	if err != nil {
		return err
	}
	store := stateStore{path: env("HACTL_STATE", "/state/hactl.json")}
	st, err := store.Load()
	if err != nil {
		return err
	}
	ops := pgxOps{
		user: env("HACTL_PG_USER", "postgres"), password: os.Getenv("HACTL_PG_PASSWORD"),
		dbname:   env("HACTL_PG_DB", "postgres"),
		replUser: env("HACTL_REPL_USER", "replicator"), replPassword: os.Getenv("HACTL_REPL_PASSWORD"),
		replPort: 5432,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c := newController(nodes, ops, store, st, time.Now, log.Printf)
	rw := newProxy(c.routeRW)
	ro := newProxy(c.routeRO)
	c.onRWChange = rw.closeAll
	log.Printf("hactl: %d nodes, state %+v", len(nodes), st)

	errc := make(chan error, 3)
	for _, l := range []struct {
		addr string
		p    *proxy
	}{{env("HACTL_RW", ":5432"), rw}, {env("HACTL_RO", ":5433"), ro}} {
		ln, err := net.Listen("tcp", l.addr)
		if err != nil {
			return err
		}
		go func(p *proxy) { errc <- p.serve(ctx, ln) }(l.p)
	}
	srv := &http.Server{Addr: env("HACTL_ADMIN", ":8008"), Handler: c.handler(ctx), ReadHeaderTimeout: 5 * time.Second}
	go func() { errc <- srv.ListenAndServe() }()

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		c.tick(ctx)
		select {
		case <-ctx.Done():
			sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(sctx)
			return nil
		case err := <-errc:
			return err
		case <-t.C:
		}
	}
}
