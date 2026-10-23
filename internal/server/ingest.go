package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
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

// maxConcurrentIngest bounds the envelopes held in memory at once. Each one
// costs several times its size while it is read, decompressed and decoded, so
// the per-request budgets above multiply by the number of pushes in flight. A
// push that finds every slot taken gets 503 with Retry-After, which the agent
// retries as a server error (internal/agent/pusher.go).
const maxConcurrentIngest = 4

// errBodyTooLarge is returned by budgetReader once the decompressed body is
// past its budget.
var errBodyTooLarge = errors.New("decompressed body exceeds the limit")

// budgetReader passes through at most budget bytes of r. A read past the
// budget fails with errBodyTooLarge instead of ending early, so the decoder
// reports an oversized body as such and not as truncated JSON.
type budgetReader struct {
	r      io.Reader
	budget int64
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.budget <= 0 {
		var probe [1]byte
		n, err := b.r.Read(probe[:])
		if n > 0 {
			return 0, errBodyTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.budget {
		p = p[:b.budget]
	}
	n, err := b.r.Read(p)
	b.budget -= int64(n)
	return n, err
}

const maxSampleAge = 12 * time.Hour
const futureSkewThreshold = 30 * time.Second

// transmittedAt reads wire.TransmittedAtHeader, falling back to the
// envelope's SentAt for agents that predate the header.
func transmittedAt(r *http.Request, sentAt time.Time) time.Time {
	if h := r.Header.Get(wire.TransmittedAtHeader); h != "" {
		if t, err := time.Parse(time.RFC3339Nano, h); err == nil {
			return t
		}
	}
	return sentAt
}

// IngestHandler decodes and validates the wire envelope, resolves identity
// through inv, and hands the (possibly filtered) envelope to pipeline for
// delta conversion and storage. inv and pipeline may be built with a nil pool
// (see NewInventory/NewPipeline) for tests that only exercise validation.
func IngestHandler(auth *Auth, inv *Inventory, pipeline *Pipeline) http.HandlerFunc {
	return ingestHandler(auth, inv, pipeline, make(chan struct{}, maxConcurrentIngest))
}

// ingestHandler is IngestHandler with the slots that bound its concurrent
// envelopes passed in, so a test can hold them.
func ingestHandler(auth *Auth, inv *Inventory, pipeline *Pipeline, slots chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Every error goes through writeError. The handler used to build its
		// JSON with Sprintf and send it with http.Error, as text/plain, with
		// the error text pasted in unescaped: a decode error quotes the bad
		// character ("invalid character '\"' ..."), which made the body
		// itself invalid JSON.
		//
		// Auth
		if !auth.Validate(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "ingest busy", "too many envelopes in flight")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer func() { _ = r.Body.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "payload too large", "")
				return
			}
			writeError(w, http.StatusBadRequest, "read error", err.Error())
			return
		}
		// The envelope is decoded from the decompressed stream, so the
		// decompressed copy is never held in memory next to the decoder's own
		// buffer.
		var src io.Reader = bytes.NewReader(body)
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
				writeError(w, http.StatusBadRequest, "invalid gzip body", gzErr.Error())
				return
			}
			defer func() { _ = gz.Close() }()
			src = &budgetReader{r: gz, budget: maxDecompressedBytes}
		}
		var env wire.Envelope
		dec := json.NewDecoder(src)
		dec.DisallowUnknownFields()
		decodeErr := dec.Decode(&env)
		// The whole decompressed body is measured, not only what the decoder
		// needed: an oversized body is refused with 413 whether its JSON is
		// valid or not, as it was before the body was decoded as a stream. The
		// agent treats 413 as "drop this, do not retry".
		_, drainErr := io.Copy(io.Discard, src)
		// The decoder can be the one that hits the budget: a value that ends one
		// byte past it needs that byte, and the budget reader's probe has already
		// consumed it, so the drain then finds nothing left to measure.
		if errors.Is(decodeErr, errBodyTooLarge) || errors.Is(drainErr, errBodyTooLarge) {
			IncIngestRejected("decompressed_too_large")
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large", "decompressed body exceeds the limit")
			return
		}
		if decodeErr != nil {
			writeError(w, http.StatusBadRequest, "bad json", decodeErr.Error())
			return
		}
		if drainErr != nil {
			writeError(w, http.StatusBadRequest, "invalid gzip body", drainErr.Error())
			return
		}
		if env.ProtocolVersion < wire.ProtocolVersionMin || env.ProtocolVersion > wire.ProtocolVersionCurrent {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported protocol_version %d, supported range %d-%d", env.ProtocolVersion, wire.ProtocolVersionMin, wire.ProtocolVersionCurrent), "")
			return
		}
		// Check sent_at age
		if time.Since(env.SentAt) > maxSampleAge {
			writeError(w, http.StatusBadRequest, "samples too old", "")
			return
		}
		// Clock skew detection (log, not reject). Measured against the
		// moment the agent transmitted the request, not SentAt: an envelope
		// replayed from the agent's buffer after an outage was queued hours
		// ago, and that delay is not skew.
		env.TransmittedAt = transmittedAt(r, env.SentAt)
		skewed := env.TransmittedAt.After(time.Now().Add(futureSkewThreshold))
		if skewed {
			ObserveAgentClockSkew(time.Since(env.TransmittedAt).Seconds())
		}

		ctx := r.Context()

		// Revocation is checked independently of the shared bootstrap
		// token above: that token authenticates "this is a pglens
		// agent," not which one — per-agent revocation (`agents.
		// revoked_at`) needs the envelope's own agent_id, only known
		// once the body is decoded, and the owner of each pushed
		// instance, because the agent_id can be left out or changed. A
		// distinct body (rather than a generic 401) lets the agent tell
		// "revoked" apart from "bad token" (internal/agent/pusher.go's
		// pushOne).
		revoked, err := inv.IsRevoked(ctx, env.AgentID)
		if err == nil && !revoked {
			revoked, err = inv.OwnerRevoked(ctx, env.Instances)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "revocation check failed", err.Error())
			return
		}
		if revoked {
			writeError(w, http.StatusUnauthorized, "revoked", "")
			return
		}

		invRes, err := inv.Upsert(ctx, env)
		if err != nil {
			IncIngestRejected("inventory_error")
			writeError(w, http.StatusInternalServerError, "inventory error", err.Error())
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
			writeError(w, http.StatusInternalServerError, "pipeline error", err.Error())
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
