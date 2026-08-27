package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// File is the on-disk shape. Written atomically, mode 0600.
type File struct {
	AgentID   uuid.UUID            `json:"agent_id"`
	Instances map[string]uuid.UUID `json:"instances"` // fingerprint -> instance id
	Version   int                  `json:"version"`   // currently 1
}

// Store manages persisted identity.
type Store struct {
	path string
	mu   sync.Mutex
	f    File
}

// Open loads the store, creating it with a fresh AgentID when absent. The
// parent directory is created with mode 0700 if missing.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("identity: mkdir %s: %w", dir, err)
	}
	// Ensure dir has correct mode (MkdirAll may not chmod existing dir)
	_ = os.Chmod(dir, 0700)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		s := &Store{
			path: path,
			f: File{
				AgentID:   uuid.New(),
				Instances: make(map[string]uuid.UUID),
				Version:   1,
			},
		}
		if err := s.persist(); err != nil {
			return nil, err
		}
		return s, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("identity: read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("identity: corrupt file %s: %w", path, err)
	}
	if f.Instances == nil {
		f.Instances = make(map[string]uuid.UUID)
	}
	if f.AgentID == uuid.Nil {
		return nil, fmt.Errorf("identity: corrupt file %s: missing agent_id", path)
	}
	// Ensure parent dir mode
	_ = os.Chmod(dir, 0700)
	return &Store{path: path, f: f}, nil
}

// AgentID returns the stable agent id.
func (s *Store) AgentID() uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.AgentID
}

// InstanceID returns the stable id for a fingerprint, allocating and
// persisting a new one on first sight. created reports whether it was
// allocated by this call.
func (s *Store) InstanceID(fingerprint string) (uuid.UUID, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.f.Instances[fingerprint]; ok {
		return id, false, nil
	}
	id := uuid.New()
	s.f.Instances[fingerprint] = id
	if err := s.persistLocked(); err != nil {
		// Rollback map on failure
		delete(s.f.Instances, fingerprint)
		return uuid.Nil, false, err
	}
	return id, true, nil
}

func (s *Store) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.f, "", "  ")
	if err != nil {
		return fmt.Errorf("identity: marshal: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("identity: write tmp: %w", err)
	}
	// Ensure mode
	_ = os.Chmod(tmp, 0600)
	// fsync the tmp file
	f, err := os.Open(tmp)
	if err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("identity: rename: %w", err)
	}
	// fsync directory
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	_ = os.Chmod(s.path, 0600)
	return nil
}
