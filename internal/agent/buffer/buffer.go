package buffer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/manprint/pglens/internal/clock"
)

// Options configures the buffer behavior.
type Options struct {
	MaxSize int64         // default 512 MiB
	MaxAge  time.Duration // default 6 hours
	Clock   clock.Clock
}

// Stats exposes buffer metrics.
type Stats struct {
	BytesUsed       int64
	Segments        int64
	SamplesDropped  map[string]int64
	CorruptRecords  int64
	BufferFull      bool
	LastDropTime    *time.Time
	LastCorruptTime *time.Time
}

// AckFunc acknowledges receipt of an envelope; caller must call it after successful send.
type AckFunc func(ctx context.Context) error

// Buffer is an append-only disk buffer with at-least-once delivery semantics.
type Buffer struct {
	dir         string
	opts        Options
	mu          sync.Mutex
	segments    []*Segment
	nextSegID   uint32
	nextReadSeg int
	nextReadOff uint64
	ackedSegID  uint32 // segment ID of last acked envelope
	ackedOff    uint64 // offset within segment of last acked envelope
	stats       Stats
}

// Open opens or creates a buffer in the given directory.
func Open(dir string, opts Options) (*Buffer, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.MaxSize == 0 {
		opts.MaxSize = 512 * 1024 * 1024 // 512 MiB
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 6 * time.Hour
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir buffer dir: %w", err)
	}

	b := &Buffer{
		dir:         dir,
		opts:        opts,
		stats:       Stats{SamplesDropped: make(map[string]int64)},
		segments:    make([]*Segment, 0),
		nextReadSeg: 0,
		nextReadOff: 0,
	}

	// Scan existing segment files and load them.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var segFiles []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".seg" {
			segFiles = append(segFiles, e.Name())
		}
	}
	sort.Strings(segFiles)

	// Load each segment and recover the next segment ID.
	for _, fname := range segFiles {
		fpath := filepath.Join(dir, fname)
		seg, err := openSegment(fpath)
		if err != nil {
			return nil, fmt.Errorf("open segment %s: %w", fname, err)
		}

		// Extract segment ID and update nextSegID.
		var segID uint32
		if _, err := fmt.Sscanf(fname, "%08x.seg", &segID); err != nil {
			return nil, fmt.Errorf("parse segment filename %s: %w", fname, err)
		}
		seg.id = segID
		seg.maxSize = segmentSize(opts.MaxSize)
		if segID >= b.nextSegID {
			b.nextSegID = segID + 1
		}

		b.segments = append(b.segments, seg)

		// Update stats.
		b.stats.Segments++
		b.stats.BytesUsed += seg.FileSize()
	}

	// Recalculate stats (recovered from disk).
	b.recalculateStats()
	b.enforceMaxAge()

	// Load acked position from metadata file.
	if err := b.loadAckedPosition(); err != nil {
		// If metadata doesn't exist, start from the beginning.
		b.ackedSegID = 0
		b.ackedOff = 0
	}

	// Start reading from just after the last acked position.
	b.nextReadSeg = 0
	b.nextReadOff = 0
	b.findSegmentAfterAcked()

	return b, nil
}

// Append appends an envelope to the buffer.
func (b *Buffer) Append(env []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Create a new segment if needed.
	if len(b.segments) == 0 {
		seg, err := b.newSegment()
		if err != nil {
			b.handleWriteError(err)
			return err
		}
		b.segments = append(b.segments, seg)
	}

	// Try to append to the last segment; if it would roll, create a new one.
	lastSeg := b.segments[len(b.segments)-1]
	if lastSeg.WouldRoll(len(env)) {
		if err := lastSeg.Sync(); err != nil {
			b.handleWriteError(err)
			return err
		}

		seg, err := b.newSegment()
		if err != nil {
			b.handleWriteError(err)
			return err
		}
		b.segments = append(b.segments, seg)
		lastSeg = seg
	}

	// Append to the current segment.
	if err := lastSeg.Append(env); err != nil {
		b.handleWriteError(err)
		return err
	}

	b.stats.BytesUsed += int64(len(env)) + 8 // 4-byte length + 4-byte CRC32C

	// Check if we need to drop old segments.
	b.enforceMaxSize()
	b.enforceMaxAge()

	// Clear buffer_full if write succeeded.
	if b.stats.BufferFull {
		b.stats.BufferFull = false
	}

	return nil
}

// Next returns the next unacknowledged envelope and an AckFunc to mark it as sent.
// Each call advances through the buffer; call ackFunc after successfully sending.
func (b *Buffer) Next() ([]byte, AckFunc, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Loop to skip corrupt records.
	for {
		// Start from nextReadSeg/nextReadOff (or after the last ack).
		for si := b.nextReadSeg; si < len(b.segments); si++ {
			seg := b.segments[si]
			env, nextOff, err := seg.ReadAt(b.nextReadOff)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, nil, err
			}

			// If we got an envelope (nextOff advanced), return it with an ack function.
			if nextOff > b.nextReadOff || si > b.nextReadSeg {
				// We have a record (possibly after skipping corrupt ones).
				if env != nil {
					// Capture the segment and offset for the ack.
					segID := seg.id
					nextOffCopy := nextOff
					ackFunc := func(ctx context.Context) error {
						b.mu.Lock()
						defer b.mu.Unlock()
						// Mark this envelope as acked.
						b.ackedSegID = segID
						b.ackedOff = nextOffCopy
						return nil
					}
					// Advance read position for the next call.
					b.nextReadOff = nextOff
					return env, ackFunc, nil
				}
				// Corrupted record (env == nil but we have an offset); keep
				// advancing. This is the one place corruption is detected, and
				// it is where CorruptRecords has to be counted: the counter
				// used to be bumped by the size/age retention policies
				// instead, so a buffer that was simply doing its job — rolling
				// off old segments under a bounded volume — reported hundreds
				// of "corrupt" records, while a genuinely unreadable record
				// (torn write, bad CRC) reported none.
				b.stats.CorruptRecords++
				corruptAt := b.opts.Clock.Now()
				b.stats.LastCorruptTime = &corruptAt
				b.nextReadOff = nextOff
				continue
			}

			// Segment caught up to its current end. Only advance past it
			// when a later segment already exists — proof this one is
			// closed and will never receive another Append. Advancing past
			// the last (still-active) segment here would permanently orphan
			// every record appended to it after this point: with a single
			// never-rolled segment (below the 8 MiB roll threshold), a
			// producer and consumer racing on the same live Buffer would
			// otherwise catch EOF once and then never see new data again.
			if si < len(b.segments)-1 {
				b.nextReadOff = 0
				b.nextReadSeg = si + 1
				continue
			}
			break
		}

		// No more records (yet) in the currently active segment.
		return nil, nil, io.EOF
	}
}

// Rewind moves the read cursor back to the last acked position, so every
// record that was handed out by Next() but never acked is offered again.
//
// Next() advances the cursor as soon as it returns a record, independently of
// the ack — which is what makes a *skipped* ack silently lossy: the record
// stays on disk and keeps counting against the size budget, but nothing ever
// offers it again for the rest of the process's life, so the documented
// at-least-once delivery degrades to at-most-once until the next restart.
// Callers that stop a drain without acking (the pusher, when the server
// answers 401, or when the push context is cancelled) call this to keep the
// guarantee.
func (b *Buffer) Rewind() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextReadSeg = 0
	b.nextReadOff = 0
	b.findSegmentAfterAcked()
}

// Stats returns current buffer statistics.
func (b *Buffer) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := b.stats
	stats.Segments = int64(len(b.segments))
	return stats
}

// Close closes all segments and persists acked position.
func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, seg := range b.segments {
		if err := seg.Close(); err != nil {
			return err
		}
	}

	// Persist acked position.
	return b.saveAckedPosition()
}

// --- private helpers ---

func (b *Buffer) newSegment() (*Segment, error) {
	segID := b.nextSegID
	fname := filepath.Join(b.dir, fmt.Sprintf("%08x.seg", segID))
	b.nextSegID++

	f, err := os.Create(fname)
	if err != nil {
		return nil, fmt.Errorf("create segment: %w", err)
	}

	seg := &Segment{
		file:      f,
		path:      fname,
		id:        segID,
		createdAt: b.opts.Clock.Now(),
		maxSize:   segmentSize(b.opts.MaxSize),
	}
	b.stats.Segments++
	return seg, nil
}

func (b *Buffer) handleWriteError(err error) {
	if isSyscallErrno(err) {
		b.stats.BufferFull = true
	}
}

func isSyscallErrno(err error) bool {
	// Check if error is a syscall error with ENOSPC
	var se *os.SyscallError
	if errors.As(err, &se) {
		return errors.Is(se.Err, syscall.ENOSPC)
	}
	return errors.Is(err, syscall.ENOSPC)
}

func (b *Buffer) recalculateStats() {
	b.stats.BytesUsed = 0
	for _, seg := range b.segments {
		b.stats.BytesUsed += seg.FileSize()
	}
}

func (b *Buffer) enforceMaxSize() {
	for b.stats.BytesUsed > b.opts.MaxSize && len(b.segments) > 0 {
		oldestSeg := b.segments[0]
		recordCount := oldestSeg.RecordCount()
		_ = oldestSeg.Close()
		_ = os.Remove(oldestSeg.path) // best-effort: a leftover file here costs disk, not correctness

		b.segments = b.segments[1:]
		b.stats.Segments--
		b.stats.BytesUsed -= oldestSeg.FileSize()
		b.stats.SamplesDropped["size_limit"] += int64(recordCount)
		now := b.opts.Clock.Now()
		b.stats.LastDropTime = &now

		// Adjust read position if we dropped the segment being read.
		if b.nextReadSeg > 0 {
			b.nextReadSeg--
		} else {
			b.nextReadOff = 0
		}
	}
}

func (b *Buffer) enforceMaxAge() {
	now := b.opts.Clock.Now()
	for len(b.segments) > 0 {
		oldestSeg := b.segments[0]
		age := now.Sub(oldestSeg.CreatedAt())
		if age <= b.opts.MaxAge {
			break
		}

		recordCount := oldestSeg.RecordCount()
		_ = oldestSeg.Close()
		_ = os.Remove(oldestSeg.path) // best-effort: a leftover file here costs disk, not correctness

		b.segments = b.segments[1:]
		b.stats.Segments--
		b.stats.BytesUsed -= oldestSeg.FileSize()
		b.stats.SamplesDropped["age_limit"] += int64(recordCount)
		now := b.opts.Clock.Now()
		b.stats.LastDropTime = &now

		// Adjust read position.
		if b.nextReadSeg > 0 {
			b.nextReadSeg--
		} else {
			b.nextReadOff = 0
		}
	}
}

// loadAckedPosition loads the acked position from metadata file.
func (b *Buffer) loadAckedPosition() error {
	metaPath := filepath.Join(b.dir, ".acked")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}

	if len(data) < 12 {
		return errors.New("invalid metadata file")
	}

	b.ackedSegID = binary.LittleEndian.Uint32(data[0:4])
	b.ackedOff = binary.LittleEndian.Uint64(data[4:12])
	return nil
}

// saveAckedPosition saves the acked position to metadata file.
func (b *Buffer) saveAckedPosition() error {
	metaPath := filepath.Join(b.dir, ".acked")
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:4], b.ackedSegID)
	binary.LittleEndian.PutUint64(data[4:12], b.ackedOff)
	return os.WriteFile(metaPath, data, 0644)
}

// findSegmentAfterAcked sets nextReadSeg and nextReadOff to point just after the acked position.
func (b *Buffer) findSegmentAfterAcked() {
	for i, seg := range b.segments {
		if seg.id < b.ackedSegID {
			// This segment is entirely before the acked position; skip it.
			continue
		}
		if seg.id == b.ackedSegID {
			// Start reading from just after the acked offset.
			b.nextReadSeg = i
			b.nextReadOff = b.ackedOff
			return
		}
		// seg.id > b.ackedSegID; start from the beginning of this segment.
		b.nextReadSeg = i
		b.nextReadOff = 0
		return
	}
}

// computeCRC32C computes the CRC32-Castagnoli checksum.
func computeCRC32C(data []byte) uint32 {
	table := crc32.MakeTable(crc32.Castagnoli)
	return crc32.Checksum(data, table)
}

// Segment represents one append-only segment file.
type Segment struct {
	file        *os.File
	path        string
	id          uint32
	createdAt   time.Time
	maxSize     int64
	recordCount int64
	fileSize    int64
	mu          sync.Mutex
}

// openSegment opens an existing segment file.
func openSegment(fpath string) (*Segment, error) {
	f, err := os.OpenFile(fpath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	seg := &Segment{
		file:      f,
		path:      fpath,
		createdAt: stat.ModTime(),
		fileSize:  stat.Size(),
	}

	// Count records in the file by scanning. A truncated or corrupt trailing
	// record is expected (a crash mid-write) and stops the scan without being
	// an error the caller should see — Open() still succeeds with whatever
	// intact records were found, per the buffer's crash-tolerance contract.
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, err
	}

scan:
	for {
		var lenBuf [4]byte
		n, readErr := f.Read(lenBuf[:])
		if n == 0 || errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = f.Close()
			return nil, readErr
		}

		length := binary.LittleEndian.Uint32(lenBuf[:])
		crcBuf := make([]byte, 4)
		if _, readErr := f.Read(crcBuf); readErr != nil {
			break scan // truncated trailing record; stop counting, not fatal
		}

		// Skip the actual data.
		if _, readErr := f.Seek(int64(length), 1); readErr != nil {
			break scan // truncated trailing record; stop counting, not fatal
		}

		seg.recordCount++
	}

	// Seek to end for appending.
	if _, err := f.Seek(0, 2); err != nil {
		_ = f.Close()
		return nil, err
	}

	return seg, nil
}

// Append appends a record to this segment.
func (s *Segment) Append(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	length := uint32(len(data))
	crc := computeCRC32C(data)

	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, length)

	if _, err := s.file.Write(lenBuf); err != nil {
		return err
	}

	creBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(creBuf, crc)

	if _, err := s.file.Write(creBuf); err != nil {
		return err
	}

	if _, err := s.file.Write(data); err != nil {
		return err
	}

	s.recordCount++
	s.fileSize += int64(len(lenBuf) + len(creBuf) + len(data))

	return nil
}

// WouldRoll reports if appending this many bytes would cause a segment roll.
func (s *Segment) WouldRoll(size int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	maxSize := s.maxSize
	if maxSize <= 0 {
		maxSize = 8 * 1024 * 1024
	}
	return s.fileSize+int64(size+8) > maxSize
}

func segmentSize(maxSize int64) int64 {
	const defaultSize = 8 * 1024 * 1024
	if maxSize > 0 && maxSize < defaultSize {
		return maxSize
	}
	return defaultSize
}

// Sync flushes the segment to disk.
func (s *Segment) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Sync()
}

// ReadAt reads a record at the given offset; returns (data, nextOffset, error).
func (s *Segment) ReadAt(offset uint64) ([]byte, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.file.Seek(int64(offset), 0)
	if err != nil {
		return nil, offset, err
	}

	var lenBuf [4]byte
	n, err := s.file.Read(lenBuf[:])
	if n == 0 || errors.Is(err, io.EOF) {
		return nil, offset, io.EOF
	}
	if err != nil {
		return nil, offset, err
	}

	length := binary.LittleEndian.Uint32(lenBuf[:])

	var crcBuf [4]byte
	n, err = s.file.Read(crcBuf[:])
	if n == 0 || errors.Is(err, io.EOF) {
		return nil, offset, io.EOF // Truncated
	}
	if err != nil {
		return nil, offset, err
	}

	data := make([]byte, length)
	n, err = s.file.Read(data)
	if int64(n) < int64(length) {
		// Truncated tail (a torn write, e.g. ENOSPC partway through this
		// record's data): unlike a corrupt/CRC-mismatched record, the file
		// genuinely does not contain `length` more bytes here, so the next
		// record does NOT start at offset+8+length — that position doesn't
		// exist yet. Treating it as a skippable record (as an earlier
		// version of this code did) advanced the read cursor past the true
		// end of file; every future ReadAt then hit an immediate premature
		// EOF at that unreachable offset, permanently stranding every
		// record already durably appended before the torn one. Matches
		// openSegment's own recovery-scan precedent: a truncated trailing
		// record just means "nothing (more) to read here yet", not "skip
		// forward" — the caller's own offset is still valid and will see
		// this record once the rest of it is actually written.
		return nil, offset, io.EOF
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset, err
	}

	expected := binary.LittleEndian.Uint32(crcBuf[:])
	actual := computeCRC32C(data)
	if expected != actual {
		// Corrupt; skip this record
		nextOffset := offset + uint64(8+length)
		return nil, nextOffset, nil
	}

	nextOffset := offset + uint64(8+length)
	return data, nextOffset, nil
}

// FileSize returns the current file size.
func (s *Segment) FileSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileSize
}

// RecordCount returns the number of records in this segment.
func (s *Segment) RecordCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordCount
}

// CreatedAt returns the segment creation time.
func (s *Segment) CreatedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createdAt
}

// Close closes the segment file.
func (s *Segment) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}
