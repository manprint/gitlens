package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/manprint/pglens/internal/wire"
)

const maxBodyBytes = 32 << 20 // 32 MiB

// maxDecompressedBytes caps the envelope *after* gzip. MaxBytesReader only
// bounds what arrives on the wire, and gzip's ratio on the repetitive JSON an
// envelope is made of is easily three orders of magnitude: a 32 MiB body that
// passes the wire limit can expand to tens of gigabytes, so without this cap
// a single authenticated request — or a corrupted one — is an out-of-memory
// kill of the whole server. 256 MiB is 8x the wire limit, comfortably above
// any real envelope (the agent's own 413 handling drops anything near it).
// A var, not a const, purely so the bomb regression test can lower it and
// assert the boundary in milliseconds instead of inflating a real 256 MiB.
// Nothing in production reassigns it. Same precedent as sessionRandRead.
var maxDecompressedBytes int64 = 256 << 20

const maxSampleAge = 12 * time.Hour
const futureSkewThreshold = 30 * time.Second

// IngestHandler decodes and validates the wire envelope, resolves identity
// through inv, and hands the (possibly filtered) envelope to pipeline for
// delta conversion and storage. inv and pipeline may be built with a nil pool
// (see NewInventory/NewPipeline) for tests that only exercise validation.
func IngestHandler(auth *Auth, inv *Inventory, pipeline *Pipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth
		if !auth.Validate(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer func() { _ = r.Body.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if err.Error() == "http: request body too large" {
				http.Error(w, `{"error":"payload too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, `{"error":"read error"}`, http.StatusBadRequest)
			return
		}
		// agent.Pusher always gzips (internal/agent/pusher.go's buildRequest)
		// and sets Content-Encoding accordingly; net/http does not
		// transparently decompress request bodies (only response bodies),
		// so this was never being undone — every real push from a real agent
		// binary failed with "bad json: invalid character '\x1f'..." (gzip's
		// magic byte), a bug no test caught before cmd/pglens-agent/run.go
		// existed to drive a real POST here.
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, gzErr := gzip.NewReader(bytes.NewReader(body))
			if gzErr != nil {
				http.Error(w, `{"error":"invalid gzip body"}`, http.StatusBadRequest)
				return
			}
			// LimitReader at maxDecompressedBytes+1 so an envelope that
			// exactly fills the budget is still accepted and only one that
			// exceeds it is rejected, with 413 (the status the agent's
			// pushOne already treats as "drop this, do not retry").
			body, err = io.ReadAll(io.LimitReader(gz, maxDecompressedBytes+1))
			_ = gz.Close()
			if err != nil {
				http.Error(w, `{"error":"invalid gzip body"}`, http.StatusBadRequest)
				return
			}
			if int64(len(body)) > maxDecompressedBytes {
				IncIngestRejected("decompressed_too_large")
				http.Error(w, `{"error":"payload too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
		}
		var env wire.Envelope
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&env); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"bad json: %v"}`, err), http.StatusBadRequest)
			return
		}
		if env.ProtocolVersion < wire.ProtocolVersionMin || env.ProtocolVersion > wire.ProtocolVersionCurrent {
			http.Error(w, fmt.Sprintf(`{"error":"unsupported protocol_version %d, supported range %d-%d"}`, env.ProtocolVersion, wire.ProtocolVersionMin, wire.ProtocolVersionCurrent), http.StatusBadRequest)
			return
		}
		// Check sent_at age
		if time.Since(env.SentAt) > maxSampleAge {
			http.Error(w, `{"error":"samples too old"}`, http.StatusBadRequest)
			return
		}
		// Clock skew detection (log, not reject)
		skewed := env.SentAt.After(time.Now().Add(futureSkewThreshold))
		if skewed {
			ObserveAgentClockSkew(time.Since(env.SentAt).Seconds())
		}

		ctx := r.Context()

		// Revocation is checked independently of the shared bootstrap
		// token above: that token authenticates "this is a pglens
		// agent," not which one — per-agent revocation (`agents.
		// revoked_at`) needs the envelope's own agent_id, only known
		// once the body is decoded. A distinct body (rather than a
		// generic 401) lets the agent tell "revoked" apart from "bad
		// token" (internal/agent/pusher.go's pushOne).
		revoked, err := inv.IsRevoked(ctx, env.AgentID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"revocation check failed: %v"}`, err), http.StatusInternalServerError)
			return
		}
		if revoked {
			http.Error(w, `{"error":"revoked"}`, http.StatusUnauthorized)
			return
		}

		invRes, err := inv.Upsert(ctx, env)
		if err != nil {
			IncIngestRejected("inventory_error")
			http.Error(w, fmt.Sprintf(`{"error":"inventory error: %v"}`, err), http.StatusInternalServerError)
			return
		}
		filtered := env
		if len(invRes.Errors) > 0 {
			instances := make([]wire.Instance, 0, len(env.Instances))
			for _, inst := range env.Instances {
				if _, rejected := invRes.Errors[inst.InstanceID]; !rejected {
					instances = append(instances, inst)
				}
			}
			filtered.Instances = instances
		}
		pipeRes, err := pipeline.Process(ctx, filtered)
		if err != nil {
			IncIngestRejected("pipeline_error")
			http.Error(w, fmt.Sprintf(`{"error":"pipeline error: %v"}`, err), http.StatusInternalServerError)
			return
		}
		accepted := pipeRes.Accepted
		rejected := pipeRes.Rejected + len(invRes.Errors)
		if rejected == 0 {
			IncIngestEnvelopes("ok")
		} else {
			IncIngestEnvelopes("partial")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"accepted":%d,"rejected":%d}`, accepted, rejected)
	}
}
