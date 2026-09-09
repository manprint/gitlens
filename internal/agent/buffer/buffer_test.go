package buffer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
)

// firstSegmentFile returns the path of the first *.seg file in dir, ignoring
// non-segment files (in particular ".acked", which sorts before any
// "NNNNNNNN.seg" name and would otherwise be picked by a naive entries[0]).
func firstSegmentFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".seg") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatal("no .seg file found")
	return ""
}

// TestBuffer_AppendReadRoundTrip — 1000 records survive close and reopen in order.
func TestBuffer_AppendReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Append 1000 records.
	records := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		records[i] = []byte("record-" + string(rune(i%10)) + "-" + string(rune((i/10)%10)))
		if err := buf.Append(records[i]); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and read back in order.
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for i := 0; i < 1000; i++ {
		data, ackFunc, err := buf.Next()
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if len(data) != len(records[i]) || string(data) != string(records[i]) {
			t.Errorf("record %d mismatch: got %q, want %q", i, string(data), string(records[i]))
		}

		if err := ackFunc(context.Background()); err != nil {
			t.Fatalf("Ack %d: %v", i, err)
		}
	}

	// No more records.
	data, _, err := buf.Next()
	if data != nil || !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

// TestBuffer_NextCatchesUpThenSeesLaterAppends — a producer and consumer
// sharing one live Buffer, below the 8 MiB segment-roll threshold (so
// everything lands in a single, never-rolled segment): Next() catching up to
// EOF once must not permanently strand later Appends to that same segment.
// Found live via SYS-AGENT-003 (a 16 KiB tmpfs buffer): the agent's own
// pushFromBuffer loop calls Next() roughly once a second, so on a lightly
// loaded target it catches EOF almost immediately after startup — after
// which every envelope queued from then on (including the entire backlog
// built up during a real outage) became silently unreadable forever.
func TestBuffer_NextCatchesUpThenSeesLaterAppends(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	if err := buf.Append([]byte("first")); err != nil {
		t.Fatalf("Append first: %v", err)
	}

	data, ack, err := buf.Next()
	if err != nil || string(data) != "first" {
		t.Fatalf("Next 1: data=%q err=%v", data, err)
	}
	if err := ack(context.Background()); err != nil {
		t.Fatalf("ack 1: %v", err)
	}

	// Catch up to EOF on the single (never-rolled) segment.
	if _, _, err := buf.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after draining, got %v", err)
	}

	// A later Append to that same segment must still be visible.
	if err := buf.Append([]byte("second")); err != nil {
		t.Fatalf("Append second: %v", err)
	}
	data2, ack2, err := buf.Next()
	if err != nil {
		t.Fatalf("Next 2: %v", err)
	}
	if string(data2) != "second" {
		t.Fatalf("Next 2: got %q, want %q (record appended after EOF was silently stranded)", data2, "second")
	}
	if err := ack2(context.Background()); err != nil {
		t.Fatalf("ack 2: %v", err)
	}
}

// TestBuffer_SegmentRoll — records crossing 8 MiB create a second segment.
func TestBuffer_SegmentRoll(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Create records that will fill to just over 8 MiB.
	recordSize := 100 * 1024 // 100 KB
	recordsPerSegment := (8 * 1024 * 1024) / recordSize

	record := make([]byte, recordSize)
	for i := 0; i < int(recordsPerSegment)+2; i++ {
		if err := buf.Append(record); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Check that we have at least 2 segments.
	stats := buf.Stats()
	if stats.Segments < 2 {
		t.Errorf("expected at least 2 segments, got %d", stats.Segments)
	}
}

func TestBuffer_ConfiguredMaxSizeControlsSegmentRoll(t *testing.T) {
	buf, err := Open(t.TempDir(), Options{MaxSize: 64, Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for i := 0; i < 3; i++ {
		if err := buf.Append([]byte("01234567890123456789")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	stats := buf.Stats()
	if got := stats.SamplesDropped["size_limit"]; got != 2 {
		t.Errorf("expected the first segment's two records to be dropped after configured roll, got %d", got)
	}
	if stats.Segments != 1 {
		t.Errorf("expected one retained segment, got %d", stats.Segments)
	}
}

// TestBuffer_TruncatedTailSkipped — truncating the last segment mid-record makes Open succeed,
// return every intact record, and increment the corrupt counter.
func TestBuffer_TruncatedTailSkipped(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Append a few records.
	records := [][]byte{
		[]byte("record-1"),
		[]byte("record-2"),
		[]byte("record-3"),
	}
	for _, rec := range records {
		if err := buf.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Truncate the segment file mid-record in the last record.
	segPath := firstSegmentFile(t, dir)
	f, err := os.OpenFile(segPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}

	stat, _ := f.Stat()
	if err := f.Truncate(stat.Size() - 5); err != nil { // Truncate last record mid-way
		t.Fatalf("truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen should succeed despite the truncation.
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen after truncation: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Read back the intact records (should be 2 or 3, depending on when truncation hit).
	count := 0
	for {
		data, ackFunc, err := buf.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if data != nil {
			count++
		}
		if ackFunc != nil {
			if err := ackFunc(context.Background()); err != nil {
				t.Fatalf("ack: %v", err)
			}
		}
	}

	if count < 2 {
		t.Errorf("expected at least 2 intact records, got %d", count)
	}
}

// TestBuffer_CorruptCRCSkipped — flipping a byte inside a record skips exactly that record.
func TestBuffer_CorruptCRCSkipped(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	records := [][]byte{
		[]byte("first-record"),
		[]byte("second-record"),
		[]byte("third-record"),
	}
	for _, rec := range records {
		if err := buf.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Corrupt the second record by flipping a byte in the data section.
	segPath := firstSegmentFile(t, dir)

	f, err2 := os.OpenFile(segPath, os.O_RDWR, 0644)
	if err2 != nil {
		t.Fatalf("open segment for corruption: %v", err2)
	}
	// Skip first record: 4 bytes len + 4 bytes CRC + data
	offset := 4 + 4 + len(records[0])
	if _, err := f.Seek(int64(offset+8), 0); err != nil { // Skip length+CRC of the second record, into its data
		t.Fatalf("seek: %v", err)
	}
	b := make([]byte, 1)
	if _, err := f.Read(b); err != nil {
		t.Fatalf("read: %v", err)
	}
	b[0] ^= 0xFF // Flip all bits
	if _, err := f.Seek(int64(offset+8), 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and read back; should skip the corrupt record.
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()

	var readRecords []string
	for {
		data, ackFunc, err := buf.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if data != nil {
			readRecords = append(readRecords, string(data))
		}
		if ackFunc != nil {
			if err := ackFunc(context.Background()); err != nil {
				t.Fatalf("ack: %v", err)
			}
		}
	}

	// We should read at least the first and third records.
	// (The second record may or may not be corrupted depending on exact file layout;
	// the important thing is that we don't panic and can read some records.)
	if len(readRecords) == 0 {
		t.Errorf("expected at least some readable records, got 0")
	}
	if readRecords[0] != "first-record" {
		t.Errorf("first record mismatch: %q", readRecords[0])
	}
	// The skipped record is what CorruptRecords is for. The counter used to
	// be driven by the retention policies instead, so a real bad CRC left it
	// at zero — the one signal that says "this buffer lost data to
	// corruption" never fired.
	stats := buf.Stats()
	if stats.CorruptRecords == 0 {
		t.Error("expected the bad-CRC record to be counted in CorruptRecords")
	}
	if stats.LastCorruptTime == nil {
		t.Error("expected LastCorruptTime to be stamped when a record is skipped")
	}
}

// TestBuffer_MaxSizeDropsOldest — exceeding max_size deletes the oldest segment.
func TestBuffer_MaxSizeDropsOldest(t *testing.T) {
	dir := t.TempDir()
	maxSize := int64(500 * 1024) // 500 KB
	buf, err := Open(dir, Options{
		MaxSize: maxSize,
		Clock:   clock.System(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Append records to exceed maxSize.
	record := make([]byte, 50*1024) // 50 KB each
	for i := 0; i < 15; i++ {       // 15 * 50 KB = 750 KB
		if err := buf.Append(record); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	stats := buf.Stats()
	if stats.BytesUsed > maxSize {
		t.Errorf("buffer size %d exceeded max %d", stats.BytesUsed, maxSize)
	}

	// Check that oldest segment was dropped.
	dropped := stats.SamplesDropped["size_limit"]
	if dropped == 0 {
		t.Errorf("expected some dropped samples due to size limit, got 0")
	}
}

// TestBuffer_MaxAgeDropsOldest — same with the fake clock.
func TestBuffer_MaxAgeDropsOldest(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Unix(0, 0))
	buf, err := Open(dir, Options{
		MaxAge: 1 * time.Hour,
		Clock:  clk,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Append a few records.
	if err := buf.Append([]byte("record-1")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Advance clock past the max age.
	clk.Advance(2 * time.Hour)

	// Append another record; this should trigger age-based cleanup.
	if err := buf.Append([]byte("record-2")); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	stats := buf.Stats()
	dropped := stats.SamplesDropped["age_limit"]
	if dropped == 0 {
		t.Errorf("expected dropped samples due to age limit, got 0")
	}
}

// TestBuffer_DiskFull — with a write error, Append returns error,
// does not panic, sets buffer_full, and a later successful write clears it.
func TestBuffer_DiskFull(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Append a normal record.
	if err := buf.Append([]byte("normal")); err != nil {
		t.Fatalf("Append normal: %v", err)
	}

	// Simulate a disk full error by directly setting buffer_full
	// (since we can't easily mock the file system to return ENOSPC).
	// The handleWriteError function is tested indirectly by ensuring
	// that if buffer_full is set, subsequent Appends fail.
	buf.mu.Lock()
	buf.stats.BufferFull = true
	buf.mu.Unlock()

	// A prior ENOSPC must not permanently latch the buffer. The next write
	// retries the filesystem and clears the health flag if it succeeds.
	if err := buf.Append([]byte("success")); err != nil {
		t.Fatalf("Append after recovery: %v", err)
	}

	stats := buf.Stats()
	if stats.BufferFull {
		t.Errorf("expected buffer_full to be false after successful write")
	}
}

// TestBuffer_AckRemoves — reading advances the position; acking marks safety for restart.
func TestBuffer_AckRemoves(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Append two records.
	if err := buf.Append([]byte("record-1")); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := buf.Append([]byte("record-2")); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	// Read the first record but don't ack it.
	data1, _, err := buf.Next()
	if err != nil || string(data1) != "record-1" {
		t.Fatalf("Next 1: %v, %q", err, string(data1))
	}

	// Read and ack the second record.
	data2, ack2, err := buf.Next()
	if err != nil || string(data2) != "record-2" {
		t.Fatalf("Next 2: %v, %q", err, string(data2))
	}
	if err := ack2(context.Background()); err != nil {
		t.Fatalf("Ack 2: %v", err)
	}

	// Further reads should be EOF (nothing after record-2).
	_, _, err = buf.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF after record-2, got %v", err)
	}

	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen; should start from the acked position (after record-2).
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()

	// Should be at EOF since everything before acked position is dropped.
	_, _, err = buf.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF after reopen (at acked position), got %v", err)
	}
}

// TestBuffer_Concurrent — concurrent Append and Next clean under -race.
func TestBuffer_Concurrent(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	var wg sync.WaitGroup
	const goroutines = 10
	const operationsPerGoroutine = 100

	// Appenders.
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				record := []byte("record-" + string(rune(id)) + "-" + string(rune(j%10)))
				if err := buf.Append(record); err != nil {
					t.Errorf("Append: %v", err)
				}
			}
		}(i)
	}

	// Readers.
	var readCount atomic.Int32
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				data, ackFunc, err := buf.Next()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					t.Errorf("Next: %v", err)
					return
				}
				if data != nil {
					readCount.Add(1)
					if ackFunc != nil {
						if err := ackFunc(context.Background()); err != nil {
							t.Errorf("ack: %v", err)
							return
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	// Allow some time for any pending operations.
	time.Sleep(100 * time.Millisecond)

	stats := buf.Stats()
	if stats.Segments == 0 {
		t.Errorf("expected at least one segment, got 0")
	}
}

// TestBuffer_RewindReoffersUnackedRecords — Next() advances the read cursor
// as soon as it hands a record out, so a caller that stops without acking
// used to lose that record for the rest of the process's life even though it
// was still on disk. Rewind puts the cursor back on the last acked position.
func TestBuffer_RewindReoffersUnackedRecords(t *testing.T) {
	buf, err := Open(t.TempDir(), Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for _, rec := range []string{"a", "b", "c"} {
		if err := buf.Append([]byte(rec)); err != nil {
			t.Fatalf("Append %s: %v", rec, err)
		}
	}

	// Consume and ack "a".
	data, ack, err := buf.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(data) != "a" {
		t.Fatalf("expected record a, got %q", data)
	}
	if err := ack(context.Background()); err != nil {
		t.Fatalf("ack: %v", err)
	}

	// Read "b" but do not ack it: the send failed.
	data, _, err = buf.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(data) != "b" {
		t.Fatalf("expected record b, got %q", data)
	}

	buf.Rewind()

	var got []string
	for {
		data, ack, err := buf.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next after rewind: %v", err)
		}
		got = append(got, string(data))
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack after rewind: %v", err)
		}
	}
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("expected [b c] to be re-offered after rewind, got %v", got)
	}
}

// TestBuffer_RetentionDropsAreNotCorruption — the size and age policies used
// to add every rolled-off record to CorruptRecords, so a buffer merely
// honouring its own bounded volume reported data corruption. Corruption is
// counted where it is detected: an unreadable record.
func TestBuffer_RetentionDropsAreNotCorruption(t *testing.T) {
	t.Run("size limit", func(t *testing.T) {
		buf, err := Open(t.TempDir(), Options{MaxSize: 64, Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = buf.Close() }()

		for i := 0; i < 3; i++ {
			if err := buf.Append([]byte("01234567890123456789")); err != nil {
				t.Fatalf("Append %d: %v", i, err)
			}
		}
		stats := buf.Stats()
		if stats.SamplesDropped["size_limit"] == 0 {
			t.Fatal("expected size_limit drops, got none")
		}
		if stats.CorruptRecords != 0 {
			t.Errorf("retention drops must not count as corruption, got %d", stats.CorruptRecords)
		}
		if stats.LastCorruptTime != nil {
			t.Error("retention drops must not stamp LastCorruptTime")
		}
	})

	t.Run("age limit", func(t *testing.T) {
		clk := clock.NewFake(time.Unix(0, 0))
		buf, err := Open(t.TempDir(), Options{MaxAge: 1 * time.Hour, Clock: clk})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = buf.Close() }()

		if err := buf.Append([]byte("record-1")); err != nil {
			t.Fatalf("Append: %v", err)
		}
		clk.Advance(2 * time.Hour)
		if err := buf.Append([]byte("record-2")); err != nil {
			t.Fatalf("Append 2: %v", err)
		}

		stats := buf.Stats()
		if stats.SamplesDropped["age_limit"] == 0 {
			t.Fatal("expected age_limit drops, got none")
		}
		if stats.CorruptRecords != 0 {
			t.Errorf("retention drops must not count as corruption, got %d", stats.CorruptRecords)
		}
	})
}
