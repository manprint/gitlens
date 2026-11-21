package pglog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSource holds log files in memory; current names the file being written.
type fakeSource struct {
	files   map[string][]byte
	current string
}

func (f *fakeSource) Current(context.Context) (string, int64, error) {
	return f.current, int64(len(f.files[f.current])), nil
}

func (f *fakeSource) Size(_ context.Context, name string) (int64, bool, error) {
	b, ok := f.files[name]
	return int64(len(b)), ok, nil
}

func (f *fakeSource) ReadAt(_ context.Context, name string, off, n int64) ([]byte, error) {
	b := f.files[name]
	end := min(off+n, int64(len(b)))
	return append([]byte(nil), b[off:end]...), nil
}

func joined(chunks []Chunk) string {
	var b bytes.Buffer
	for _, c := range chunks {
		b.Write(c.Data)
	}
	return b.String()
}

// PGL-009: the first run starts at the end of the current file.
func TestPGL009_FirstRunStartsAtEnd(t *testing.T) {
	t.Parallel()
	src := &fakeSource{files: map[string][]byte{"a.json": []byte("old\nlines\n")}, current: "a.json"}
	chunks, next, skipped, err := Advance(t.Context(), src, Position{}, true, 1024)
	require.NoError(t, err)
	require.Empty(t, chunks)
	require.Zero(t, skipped)
	require.Equal(t, Position{File: "a.json", Offset: 10}, next)

	src.files["a.json"] = append(src.files["a.json"], "new\n"...)
	chunks, next, _, err = Advance(t.Context(), src, next, false, 1024)
	require.NoError(t, err)
	require.Equal(t, "new\n", joined(chunks))
	require.Equal(t, Position{File: "a.json", Offset: 14}, next)
}

// PGL-010: after rotation the rest of the old file is read, then the new one
// from 0; a partial record at the end of the old file is not read again.
func TestPGL010_Rotation(t *testing.T) {
	t.Parallel()
	src := &fakeSource{files: map[string][]byte{
		"a.json": []byte("1\n2\n3\n"),
		"b.json": []byte("4\n5"),
	}, current: "b.json"}
	chunks, next, _, err := Advance(t.Context(), src, Position{File: "a.json", Offset: 2}, false, 1024)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, "2\n3\n4\n5", joined(chunks))
	require.True(t, chunks[0].Ended)
	require.False(t, chunks[1].Ended)
	require.Equal(t, Position{File: "b.json", Offset: 3}, next)
	require.Equal(t, Position{File: "b.json", Offset: 2}, Settle(next, chunks[1], 2))

	// Budget ends inside the old file: resume there.
	chunks, next, _, err = Advance(t.Context(), src, Position{File: "a.json", Offset: 0}, false, 4)
	require.NoError(t, err)
	require.Equal(t, "1\n2\n", joined(chunks))
	require.False(t, chunks[0].Ended)
	require.Equal(t, Position{File: "a.json", Offset: 4}, next)

	// The old file is gone: the new one is read from 0.
	delete(src.files, "a.json")
	chunks, _, _, err = Advance(t.Context(), src, Position{File: "a.json", Offset: 4}, false, 1024)
	require.NoError(t, err)
	require.Equal(t, "4\n5", joined(chunks))
}

// PGL-011: a file shorter than the saved offset was truncated: read from 0.
func TestPGL011_Truncation(t *testing.T) {
	t.Parallel()
	src := &fakeSource{files: map[string][]byte{"a.json": []byte("x\n")}, current: "a.json"}
	chunks, next, _, err := Advance(t.Context(), src, Position{File: "a.json", Offset: 100}, false, 1024)
	require.NoError(t, err)
	require.Equal(t, "x\n", joined(chunks))
	require.Equal(t, Position{File: "a.json", Offset: 2}, next)
}

// PGL-012: the file source refuses names leaving its directory.
func TestPGL012_FileSourceRefusesOutsideNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.json"), []byte("abc\n"), 0o600))
	cur := "a.json"
	src := NewFileSource(dir, func(context.Context) (string, error) { return cur, nil })
	name, size, err := src.Current(t.Context())
	require.NoError(t, err)
	require.Equal(t, "a.json", name)
	require.EqualValues(t, 4, size)
	data, err := src.ReadAt(t.Context(), "a.json", 1, 10)
	require.NoError(t, err)
	require.Equal(t, "bc\n", string(data))
	for _, bad := range []string{"../x", "/etc/passwd", "sub/a.json", ".."} {
		_, err := src.ReadAt(t.Context(), bad, 0, 1)
		require.ErrorIs(t, err, ErrBadLogName, bad)
		_, _, err = src.Size(t.Context(), bad)
		require.ErrorIs(t, err, ErrBadLogName, bad)
	}
	_, ok, err := src.Size(t.Context(), "gone.json")
	require.NoError(t, err)
	require.False(t, ok)
	cur = "../a.json"
	_, _, err = src.Current(t.Context())
	require.ErrorIs(t, err, ErrBadLogName)
}

// PGL-013: positions survive save and load; a broken file gives an empty map.
func TestPGL013_Positions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "pglog-state.json")
	m, err := LoadPositions(p)
	require.NoError(t, err)
	require.Empty(t, m)
	want := map[string]Position{"t1": {File: "a.json", Offset: 42}, "t2": {File: "b.csv", Offset: 7}}
	require.NoError(t, SavePositions(p, want))
	st, err := os.Stat(p)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	got, err := LoadPositions(p)
	require.NoError(t, err)
	require.Equal(t, want, got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file left")

	require.NoError(t, os.WriteFile(p, []byte("{broken"), 0o600))
	got, err = LoadPositions(p)
	require.Error(t, err)
	require.Empty(t, got)
}

// PGL-014 (I-9): the budget bounds a read; a backlog over 16 budgets is
// skipped and reported.
func TestPGL014_Budget(t *testing.T) {
	t.Parallel()
	const mib = 1 << 20
	src := &fakeSource{files: map[string][]byte{"a.json": make([]byte, 3*mib)}, current: "a.json"}
	chunks, next, skipped, err := Advance(t.Context(), src, Position{File: "a.json"}, false, mib)
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, joined(chunks), mib)
	require.Equal(t, Position{File: "a.json", Offset: mib}, next)

	src.files["a.json"] = make([]byte, 20*mib)
	chunks, next, skipped, err = Advance(t.Context(), src, Position{File: "a.json", Offset: 0}, false, mib)
	require.NoError(t, err)
	require.Empty(t, chunks)
	require.EqualValues(t, 20*mib, skipped)
	require.Equal(t, Position{File: "a.json", Offset: 20 * mib}, next)
}
