package main

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// proxy splices TCP connections to the backend chosen at connect time.
// closeAll drops every open connection; the generation counter also drops a
// connection whose route was decided before the closeAll but dialled after.
type proxy struct {
	route       func() (string, bool)
	dialTimeout time.Duration

	mu    sync.Mutex
	gen   uint64
	conns map[net.Conn]struct{}
}

func newProxy(route func() (string, bool)) *proxy {
	return &proxy{route: route, dialTimeout: 2 * time.Second, conns: map[net.Conn]struct{}{}}
}

func (p *proxy) serve(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil //nolint:nilerr // the listener is closed on shutdown
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go p.handle(c)
	}
}

func (p *proxy) handle(client net.Conn) {
	p.mu.Lock()
	gen := p.gen
	p.mu.Unlock()
	addr, ok := p.route()
	if !ok {
		_ = client.Close()
		return
	}
	backend, err := net.DialTimeout("tcp", addr, p.dialTimeout)
	if err != nil {
		_ = client.Close()
		return
	}
	p.mu.Lock()
	if p.gen != gen {
		p.mu.Unlock()
		_ = client.Close()
		_ = backend.Close()
		return
	}
	p.conns[client] = struct{}{}
	p.conns[backend] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.conns, client)
		delete(p.conns, backend)
		p.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backend, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, backend); done <- struct{}{} }()
	<-done
	_ = client.Close()
	_ = backend.Close()
	<-done
}

func (p *proxy) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gen++
	for c := range p.conns {
		_ = c.Close()
	}
}

func (p *proxy) open() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns) / 2
}
