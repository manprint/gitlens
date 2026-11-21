package pglog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadPositions reads the saved positions, keyed by target. A missing file
// gives an empty map; a broken one gives an empty map and an error to log.
func LoadPositions(path string) (map[string]Position, error) {
	m := map[string]Position{}
	data, err := os.ReadFile(path) //nolint:gosec // operator-configured state path
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]Position{}, fmt.Errorf("pglog: positions %s: %w", path, err)
	}
	return m, nil
}

// SavePositions writes the positions through a temporary file in the same
// directory (os.CreateTemp makes it 0600) renamed over path.
func SavePositions(path string, m map[string]Position) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pglog-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
