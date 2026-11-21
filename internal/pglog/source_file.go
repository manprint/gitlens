package pglog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type fileSource struct {
	dir     string
	current func(ctx context.Context) (string, error)
}

// NewFileSource reads log files under dir. current names the file the server
// writes now as a bare file name in dir; names with "/" or ".." are refused.
func NewFileSource(dir string, current func(ctx context.Context) (string, error)) Source {
	return &fileSource{dir: dir, current: current}
}

func (s *fileSource) path(name string) (string, error) {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") || filepath.IsAbs(name) {
		return "", fmt.Errorf("%w: %q", ErrBadLogName, name)
	}
	return filepath.Join(s.dir, name), nil
}

func (s *fileSource) Current(ctx context.Context) (string, int64, error) {
	name, err := s.current(ctx)
	if err != nil {
		return "", 0, err
	}
	size, ok, err := s.Size(ctx, name)
	if err != nil {
		return "", 0, err
	}
	if !ok {
		return "", 0, fmt.Errorf("%w: %s missing", ErrNoLogFile, name)
	}
	return name, size, nil
}

func (s *fileSource) Size(_ context.Context, name string) (int64, bool, error) {
	p, err := s.path(name)
	if err != nil {
		return 0, false, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return st.Size(), true, nil
}

func (s *fileSource) ReadAt(_ context.Context, name string, off, n int64) ([]byte, error) {
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) //nolint:gosec // the name is checked to stay under the log directory
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}
