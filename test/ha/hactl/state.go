package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type promotingState struct {
	Node         string    `json:"node"`
	TS           time.Time `json:"ts"`
	FromTimeline int       `json:"from_timeline"`
}

// persisted is everything that must survive a controller restart: losing
// maxTimeline would let a stale former primary look like the leader.
// StandbyTL keeps the highest timeline seen per standby: a standby without a
// WAL receiver reports its older checkpoint timeline.
type persisted struct {
	LastLeader     string          `json:"lastLeader"`
	MaxTimeline    int             `json:"maxTimeline"`
	Promoting      *promotingState `json:"promoting,omitempty"`
	FailoverPaused bool            `json:"failoverPaused"`
	WritesBlocked  bool            `json:"writesBlocked"`
	StandbyTL      map[string]int  `json:"standbyTimeline,omitempty"`
}

type stateSaver interface {
	Save(p persisted) error
}

type stateStore struct{ path string }

// Load returns the zero state when the file does not exist yet. A file that
// exists but cannot be parsed is an error: starting without it could route
// writes to a stale node.
func (s stateStore) Load() (persisted, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return persisted{}, nil
	}
	if err != nil {
		return persisted{}, err
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return persisted{}, fmt.Errorf("state file %s: %w", s.path, err)
	}
	return p, nil
}

// Save writes atomically: temp file, fsync, rename, fsync of the directory.
func (s stateStore) Save(p persisted) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	f, err := os.CreateTemp(dir, ".hactl-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
