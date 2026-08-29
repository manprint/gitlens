package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type receiver struct {
	mu       sync.Mutex
	bodies   []json.RawMessage
	failNext int
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/_received" && req.Method == http.MethodGet {
		r.mu.Lock()
		out := make([]json.RawMessage, len(r.bodies))
		copy(out, r.bodies)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	if req.URL.Path == "/_reset" && req.Method == http.MethodPost {
		r.mu.Lock()
		r.bodies = nil
		r.failNext = 0
		r.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.URL.Path == "/_fail_next" && req.Method == http.MethodPost {
		n, _ := strconv.Atoi(req.URL.Query().Get("n"))
		if n < 0 {
			n = 0
		}
		r.mu.Lock()
		r.failNext = n
		r.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodPost {
		http.NotFound(w, req)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	if r.failNext > 0 {
		r.failNext--
		r.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	r.bodies = append(r.bodies, json.RawMessage(body))
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	log.Fatal(http.ListenAndServe(":9099", &receiver{}))
}
