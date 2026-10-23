package buffer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

	// Truncating 5 bytes tears the third record, so the first two are the only
	// intact ones and both must come back.
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

	if count != 2 {
		t.Errorf("expected exactly 2 intact records, got %d", count)
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

	// Only the flipped record is lost: the first and the third are delivered, in
	// order. A reader that stops at the corrupt record would return just the first.
	if want := []string{"first-record", "third-record"}; !reflect.DeepEqual(readRecords, want) {
		t.Errorf("delivered %q, want %q", readRecords, want)
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

// TestBuffer_DiskFull — a write that fails with ENOSPC makes Append return
// the error and sets buffer_full, and a later successful write clears it.
func TestBuffer_DiskFull(t *testing.T) {
	// /dev/full fails every write with ENOSPC, the error a full volume returns.
	full, err := os.OpenFile("/dev/full", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Skipf("no /dev/full to inject ENOSPC: %v", err)
	}
	defer func() { _ = full.Close() }()

	buf, err := Open(t.TempDir(), Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	buf.mu.Lock()
	buf.segments = []*Segment{{file: full, path: full.Name()}}
	buf.mu.Unlock()
	if err := buf.Append([]byte("full")); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Append on a full disk = %v, want ENOSPC", err)
	}
	if !buf.Stats().BufferFull {
		t.Error("expected buffer_full after ENOSPC")
	}

	// Space comes back. The next write goes to a new segment on the real disk,
	// and it clears the flag: a prior ENOSPC must not latch the buffer.
	buf.mu.Lock()
	buf.segments = nil
	buf.mu.Unlock()
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

// TestBuffer_AppendAfterPartialDrainKeepsEveryRecord is the regression test
// for the segment descriptor sharing its file offset between reads and
// writes. Next() used to Seek() the shared *os.File to just past the record
// it returned; a segment created with os.Create (no O_APPEND) then had its
// next Append land at that offset, *inside* the file, silently overwriting
// records that were already durably written.
//
// The interleaving below is not exotic — it is exactly what the pusher does
// on every drain that stops early: read one record, stop (401, or a cancelled
// push context), rewind, and keep queueing new envelopes.
func TestBuffer_AppendAfterPartialDrainKeepsEveryRecord(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	want := []string{"alpha-0000000000", "bravo-1111111111", "charlie-222222222"}
	for _, rec := range want {
		if err := buf.Append([]byte(rec)); err != nil {
			t.Fatalf("Append %q: %v", rec, err)
		}
	}

	// Read exactly one record and stop without acking: the read cursor now
	// sits between record 1 and record 2.
	data, _, err := buf.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(data) != want[0] {
		t.Fatalf("first Next = %q, want %q", data, want[0])
	}
	buf.Rewind()

	// Queue another envelope, as the agent's collection loop keeps doing
	// while delivery is stalled.
	want = append(want, "delta-33333333333")
	if err := buf.Append([]byte(want[3])); err != nil {
		t.Fatalf("Append after partial drain: %v", err)
	}

	var got []string
	for {
		data, ack, err := buf.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("drain Next: %v", err)
		}
		if data == nil {
			t.Fatalf("drain returned a corrupt record; the buffer was overwritten in place")
		}
		got = append(got, string(data))
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}

	if len(got) != len(want) {
		t.Fatalf("drained %d records %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuffer_ConcurrentProducerConsumerPreservesRecords drives a producer and
// a consumer against one live buffer and asserts on the bytes, not just on
// "a segment exists". Every record read back must be one that was written,
// verbatim, and no record may be delivered twice: the previous concurrency
// test asserted neither, which is how in-place overwriting stayed invisible.
func TestBuffer_ConcurrentProducerConsumerPreservesRecords(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	const records = 500
	want := make(map[string]bool, records)
	for i := 0; i < records; i++ {
		want[fmt.Sprintf("record-%06d-payload-padding", i)] = true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < records; i++ {
			rec := fmt.Sprintf("record-%06d-payload-padding", i)
			if err := buf.Append([]byte(rec)); err != nil {
				t.Errorf("Append: %v", err)
				return
			}
		}
	}()

	seen := make(map[string]bool, records)
	var mu sync.Mutex
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			data, ack, err := buf.Next()
			if errors.Is(err, io.EOF) {
				continue
			}
			if err != nil {
				t.Errorf("Next: %v", err)
				return
			}
			if data == nil {
				t.Errorf("corrupt record read back from a buffer nothing corrupted")
				return
			}
			mu.Lock()
			rec := string(data)
			if seen[rec] {
				t.Errorf("record %q delivered twice", rec)
			}
			seen[rec] = true
			n := len(seen)
			mu.Unlock()
			if err := ack(context.Background()); err != nil {
				t.Errorf("ack: %v", err)
				return
			}
			if n == records {
				return
			}
		}
	}()

	deadline := time.After(30 * time.Second)
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-deadline:
		close(done)
		<-drained
		t.Fatalf("timed out: consumer saw %d of %d records", len(seen), records)
	}

	for rec := range seen {
		if !want[rec] {
			t.Errorf("read back a record that was never written: %q", rec)
		}
	}
	if len(seen) != records {
		t.Errorf("read %d records, want %d", len(seen), records)
	}
}

// TestBuffer_StatsSnapshotIsIndependent — Stats() used to hand out the live
// SamplesDropped map. Pusher.DroppedSamples ranges over it on the push
// goroutine while Append writes it on the collection goroutine, which the Go
// runtime turns into an unrecoverable "concurrent map read and map write"
// crash of the whole agent.
func TestBuffer_StatsSnapshotIsIndependent(t *testing.T) {
	buf, err := Open(t.TempDir(), Options{MaxSize: 64, Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for i := 0; i < 20; i++ {
		if err := buf.Append([]byte("0123456789abcdef0123456789")); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	snap := buf.Stats()
	if len(snap.SamplesDropped) == 0 {
		t.Fatalf("expected retention drops with a 64-byte budget, got none")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			if err := buf.Append([]byte("0123456789abcdef0123456789")); err != nil {
				t.Errorf("Append: %v", err)
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		for k, v := range snap.SamplesDropped { // must not race with Append
			_, _ = k, v
		}
	}
	<-done
}

// TestBuffer_QuietAgentNeverLosesTheFreshRecord covers a low-volume agent
// whose single segment stays open for longer than MaxAge: every envelope must
// still be readable right after it is appended, and envelopes that were
// delivered must never be reported as dropped.
func TestBuffer_QuietAgentNeverLosesTheFreshRecord(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	buf, err := Open(t.TempDir(), Options{MaxAge: time.Hour, Clock: clk})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for i := 0; i < 20; i++ {
		clk.Advance(10 * time.Minute)
		record := []byte(fmt.Sprintf("envelope-%d", i))
		if err := buf.Append(record); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		got, ack, err := buf.Next()
		if err != nil {
			t.Fatalf("envelope %d was not readable right after Append: %v", i, err)
		}
		if string(got) != string(record) {
			t.Fatalf("Next = %q, want %q", got, record)
		}
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack %d: %v", i, err)
		}
	}
	for reason, n := range buf.Stats().SamplesDropped {
		if n != 0 {
			t.Errorf("%d delivered envelopes reported as dropped (%s)", n, reason)
		}
	}
}

// TestBuffer_DeliveredRecordsAreNotDrops keeps retention of already-acked
// data out of pglens_samples_dropped_rate.
func TestBuffer_DeliveredRecordsAreNotDrops(t *testing.T) {
	buf, err := Open(t.TempDir(), Options{MaxSize: 64, Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	for i := 0; i < 2; i++ {
		if err := buf.Append([]byte("01234567890123456789")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		_, ack, err := buf.Next()
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack %d: %v", i, err)
		}
	}
	// Rolls the first (fully delivered) segment out under the 64-byte budget.
	if err := buf.Append([]byte("01234567890123456789")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := buf.Stats().SamplesDropped["size_limit"]; got != 0 {
		t.Errorf("size_limit drops = %d, want 0: both removed records had been delivered", got)
	}
}

// TestBuffer_AckSurvivesAnUncleanExit reopens the buffer without Close, as
// after a crash or SIGKILL: delivered envelopes must not be replayed.
func TestBuffer_AckSurvivesAnUncleanExit(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, r := range []string{"delivered-1", "delivered-2", "pending"} {
		if err := buf.Append([]byte(r)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		_, ack, err := buf.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}
	// No Close: the process dies here. Segment data reaches the page cache on
	// every write, which is what a reopen in the same kernel observes.
	reopened, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	got, _, err := reopened.Next()
	if err != nil {
		t.Fatalf("Next after reopen: %v", err)
	}
	if string(got) != "pending" {
		t.Fatalf("first record after reopen = %q, want %q (delivered envelopes replayed)", got, "pending")
	}
}

// drainAndAck reads every record the buffer will offer, acking each one, and
// returns their payloads in order.
func drainAndAck(t *testing.T, buf *Buffer) []string {
	t.Helper()
	var got []string
	for {
		data, ack, err := buf.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if data == nil {
			t.Fatalf("Next returned a corrupt record after %q", got)
		}
		got = append(got, string(data))
		if err := ack(context.Background()); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}
}

// TestBuffer_AppendAfterTornTailKeepsLaterRecords — AB-02: a crash that left a
// torn record at the end of the active segment must not hide the records
// appended after the restart. The reader used to take their bytes for the torn
// record's payload, fail the CRC and never realign.
func TestBuffer_AppendAfterTornTailKeepsLaterRecords(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := buf.Append([]byte(strings.Repeat(string(rune('a'+i)), 40))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Cut the last record short, as a crash mid-write does.
	segPath := firstSegmentFile(t, dir)
	stat, err := os.Stat(segPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := os.Truncate(segPath, stat.Size()-5); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen after torn tail: %v", err)
	}
	for i := 3; i < 5; i++ {
		if err := buf.Append([]byte(strings.Repeat(string(rune('a'+i)), 40))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if got := buf.Stats().CorruptRecords; got != 1 {
		t.Errorf("CorruptRecords after torn tail = %d, want 1", got)
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := []string{
		strings.Repeat("a", 40),
		strings.Repeat("b", 40),
		strings.Repeat("d", 40),
		strings.Repeat("e", 40),
	}
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()
	got := drainAndAck(t, buf)
	if len(got) != len(want) {
		t.Fatalf("delivered %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuffer_AppendAfterPartialWriteKeepsLaterRecords — AB-02 and RO-09: bytes
// that a write left past the last complete record, the header of a record whose
// payload never arrived, must not sit between the records before and after them.
// The in-process shape: no restart, so only Append can cut the garbage off.
func TestBuffer_AppendAfterPartialWriteKeepsLaterRecords(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	if err := buf.Append([]byte("rec-A")); err != nil {
		t.Fatalf("Append rec-A: %v", err)
	}

	// A header claiming 1000 payload bytes, followed by 100 of them.
	torn := make([]byte, 8+100)
	binary.LittleEndian.PutUint32(torn[0:4], 1000)
	f, err := os.OpenFile(firstSegmentFile(t, dir), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := f.Write(torn); err != nil {
		t.Fatalf("write torn record: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, rec := range []string{"rec-B", "rec-C"} {
		if err := buf.Append([]byte(rec)); err != nil {
			t.Fatalf("Append %s: %v", rec, err)
		}
	}

	got := drainAndAck(t, buf)
	want := []string{"rec-A", "rec-B", "rec-C"}
	if len(got) != len(want) {
		t.Fatalf("delivered %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuffer_CorruptAckedPrefixKeepsUnackedRecords — AB-02: a flipped length
// header inside the acknowledged prefix used to stop the scan of the segment on
// reopen, and the torn-tail cut then removed every record after it, including
// the ones nobody had acknowledged yet. The reader never reads the acknowledged
// prefix, so the records after the checkpoint must survive the reopen.
func TestBuffer_CorruptAckedPrefixKeepsUnackedRecords(t *testing.T) {
	// Five records of 16 bytes each (an 8-byte header and an 8-byte payload).
	// The first three are acknowledged, so the checkpoint is at byte 48.
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		for i := 0; i < 5; i++ {
			if err := buf.Append([]byte(fmt.Sprintf("rec-%d---", i))); err != nil {
				t.Fatalf("Append %d: %v", i, err)
			}
		}
		for i := 0; i < 3; i++ {
			data, ack, err := buf.Next()
			if err != nil || data == nil {
				t.Fatalf("Next %d: %q, %v", i, data, err)
			}
			if err := ack(context.Background()); err != nil {
				t.Fatalf("ack %d: %v", i, err)
			}
		}
		if err := buf.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		// Set the length header of rec-1, which lies inside the acknowledged prefix.
		f, err := os.OpenFile(firstSegmentFile(t, dir), os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("open segment: %v", err)
		}
		header := make([]byte, 4)
		binary.LittleEndian.PutUint32(header, 0xFFFFFF00)
		if _, err := f.WriteAt(header, 16); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		return dir
	}

	t.Run("unacknowledged records survive", func(t *testing.T) {
		dir := setup(t)
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer func() { _ = buf.Close() }()
		if err := buf.Append([]byte("rec-5---")); err != nil {
			t.Fatalf("Append rec-5: %v", err)
		}
		got := drainAndAck(t, buf)
		want := []string{"rec-3---", "rec-4---", "rec-5---"}
		if len(got) != len(want) {
			t.Fatalf("delivered %q, want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("record %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("a torn tail after the corrupt prefix is still cut", func(t *testing.T) {
		dir := setup(t)
		// Cut rec-4 short, as a crash mid-write does.
		segPath := firstSegmentFile(t, dir)
		stat, err := os.Stat(segPath)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if err := os.Truncate(segPath, stat.Size()-3); err != nil {
			t.Fatalf("Truncate: %v", err)
		}
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer func() { _ = buf.Close() }()
		if n := buf.Stats().CorruptRecords; n != 1 {
			t.Errorf("CorruptRecords = %d, want 1 for the torn rec-4", n)
		}
		if n := buf.Stats().CutBytes; n != 13 {
			t.Errorf("CutBytes = %d, want 13: rec-4 keeps 13 of its 16 bytes", n)
		}
		if err := buf.Append([]byte("rec-5---")); err != nil {
			t.Fatalf("Append rec-5: %v", err)
		}
		got := drainAndAck(t, buf)
		want := []string{"rec-3---", "rec-5---"}
		if len(got) != len(want) {
			t.Fatalf("delivered %q, want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("record %d = %q, want %q", i, got[i], want[i])
			}
		}
	})
}

// TestBuffer_CorruptRecordInClosedSegmentKeepsNeighbours — AB-01: a corrupt
// record in a closed segment is skipped on its own. The reader used to move on
// to the next segment at the corrupt record's offset, so it could ack a record
// misread from there. Once that ack moved the checkpoint forward, the closed
// segment was deleted with its undelivered records and nothing counted the loss.
func TestBuffer_CorruptRecordInClosedSegmentKeepsNeighbours(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// 127 records of this size fill the first 8 MiB segment; the rest roll over.
	const total = 200
	const recSize = 64 * 1024
	numbered := func(i int) []byte {
		rec := make([]byte, recSize)
		copy(rec, fmt.Sprintf("rec-%03d:", i))
		return rec
	}
	for i := 0; i < total; i++ {
		if err := buf.Append(numbered(i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Flip one payload byte of record 10, which sits in the closed first segment.
	segPath := firstSegmentFile(t, dir)
	f, err := os.OpenFile(segPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	off := int64(10*(8+recSize) + 8 + 100)
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, off); err != nil {
		t.Fatalf("read: %v", err)
	}
	b[0] ^= 0xFF
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()

	got := drainAndAck(t, buf)
	var want []int
	for i := 0; i < total; i++ {
		if i != 10 {
			want = append(want, i)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("delivered %d records, want %d", len(got), len(want))
	}
	for i, idx := range want {
		prefix := fmt.Sprintf("rec-%03d:", idx)
		if !strings.HasPrefix(got[i], prefix) {
			t.Fatalf("delivery %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
		}
	}
	if n := buf.Stats().CorruptRecords; n != 1 {
		t.Errorf("CorruptRecords = %d, want 1", n)
	}
}

// TestBuffer_StaleCheckpointKeepsNewSegments — AB-03: the segment files are
// removed while .acked survives, as when an operator clears the buffer
// directory to free space. The checkpoint then points past everything on disk.
// New segments are numbered from the files alone, so they land below the
// checkpoint, look acknowledged, and the next Open deletes them unread.
func TestBuffer_StaleCheckpointKeepsNewSegments(t *testing.T) {
	for _, tc := range []struct {
		name       string
		closeFirst bool
		fresh      int
	}{
		{name: "closed before restart", closeFirst: true, fresh: 200},
		{name: "unclean exit before restart", closeFirst: false, fresh: 200},
		// 400 and 600 fresh records number new segments through the checkpoint's
		// own ID and past it, so the stale offset lands inside a new segment.
		{name: "unclean exit after 400 fresh records", closeFirst: false, fresh: 400},
		{name: "unclean exit after 600 fresh records", closeFirst: false, fresh: 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			buf, err := Open(dir, Options{Clock: clock.System()})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			// 400 records of this size span four segments; draining them all
			// leaves the checkpoint in the last one.
			const recSize = 64 * 1024
			for i := 0; i < 400; i++ {
				rec := make([]byte, recSize)
				copy(rec, fmt.Sprintf("old-%03d:", i))
				if err := buf.Append(rec); err != nil {
					t.Fatalf("Append %d: %v", i, err)
				}
			}
			if got := len(drainAndAck(t, buf)); got != 400 {
				t.Fatalf("drained %d old records, want 400", got)
			}
			if err := buf.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			// The cleanup: every segment file goes, .acked stays.
			segs, err := filepath.Glob(filepath.Join(dir, "*.seg"))
			if err != nil || len(segs) == 0 {
				t.Fatalf("no segment files to remove: %v", err)
			}
			for _, s := range segs {
				if err := os.Remove(s); err != nil {
					t.Fatalf("remove %s: %v", s, err)
				}
			}

			buf, err = Open(dir, Options{Clock: clock.System()})
			if err != nil {
				t.Fatalf("Open after cleanup: %v", err)
			}
			for i := 0; i < tc.fresh; i++ {
				rec := make([]byte, recSize)
				copy(rec, fmt.Sprintf("new-%03d:", i))
				if err := buf.Append(rec); err != nil {
					t.Fatalf("Append new %d: %v", i, err)
				}
			}
			if tc.closeFirst {
				if err := buf.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
			}

			buf, err = Open(dir, Options{Clock: clock.System()})
			if err != nil {
				t.Fatalf("second Open: %v", err)
			}
			defer func() { _ = buf.Close() }()
			got := drainAndAck(t, buf)
			if len(got) != tc.fresh {
				t.Fatalf("delivered %d records written after the cleanup, want %d", len(got), tc.fresh)
			}
			for i := range got {
				if prefix := fmt.Sprintf("new-%03d:", i); !strings.HasPrefix(got[i], prefix) {
					t.Fatalf("record %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
				}
			}
			// Nothing undelivered may be dropped silently on the way.
			if dropped := buf.Stats().SamplesDropped; len(dropped) != 0 {
				t.Errorf("SamplesDropped = %v after the cleanup, want none", dropped)
			}
		})
	}
}

// TestBuffer_CheckpointPastUnackedSegmentsKeepsThem — AB-03: the checkpoint names
// a segment past every segment on disk, while the segments below it hold records
// that were never delivered. A pre-fix binary or a manual edit can leave such a
// checkpoint behind, and the fixed binary must not make that state worse. The
// records must still be delivered. The restart that moves the checkpoint back
// must be persisted: otherwise an unclean exit after new appends would find the
// old checkpoint again and delete the old records unread. When that write is
// blocked, the stale checkpoint is removed instead, with the same outcome.
func TestBuffer_CheckpointPastUnackedSegmentsKeepsThem(t *testing.T) {
	const recSize = 64 * 1024
	// setup leaves 200 records of recSize bytes in segments 0 and 1, none acked,
	// and a checkpoint that names segment 5.
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		for i := 0; i < 200; i++ {
			rec := make([]byte, recSize)
			copy(rec, fmt.Sprintf("old-%03d:", i))
			if err := buf.Append(rec); err != nil {
				t.Fatalf("Append %d: %v", i, err)
			}
		}
		if err := buf.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		meta := make([]byte, 12)
		binary.LittleEndian.PutUint32(meta[0:4], 5)
		if err := os.WriteFile(filepath.Join(dir, ".acked"), meta, 0o644); err != nil {
			t.Fatalf("write checkpoint: %v", err)
		}
		return dir
	}

	t.Run("old records are delivered after a clean restart", func(t *testing.T) {
		dir := setup(t)
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = buf.Close() }()
		got := drainAndAck(t, buf)
		if len(got) != 200 {
			t.Fatalf("delivered %d records, want 200", len(got))
		}
		for i := range got {
			if prefix := fmt.Sprintf("old-%03d:", i); !strings.HasPrefix(got[i], prefix) {
				t.Fatalf("record %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
			}
		}
		if dropped := buf.Stats().SamplesDropped; len(dropped) != 0 {
			t.Errorf("SamplesDropped = %v, want none", dropped)
		}
	})

	t.Run("old records survive an unclean exit after rolled appends", func(t *testing.T) {
		dir := setup(t)
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		// The new records fill the rest of segment 1 and roll into segments numbered
		// past the checkpoint.
		for i := 0; i < 200; i++ {
			rec := make([]byte, recSize)
			copy(rec, fmt.Sprintf("new-%03d:", i))
			if err := buf.Append(rec); err != nil {
				t.Fatalf("Append new %d: %v", i, err)
			}
		}
		// No Close: the checkpoint on disk is the one Open wrote.
		reopened, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		defer func() { _ = reopened.Close() }()
		got := drainAndAck(t, reopened)
		if len(got) != 400 {
			t.Fatalf("delivered %d records, want 400", len(got))
		}
		for i := range got {
			prefix := fmt.Sprintf("old-%03d:", i)
			if i >= 200 {
				prefix = fmt.Sprintf("new-%03d:", i-200)
			}
			if !strings.HasPrefix(got[i], prefix) {
				t.Fatalf("record %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
			}
		}
		if dropped := reopened.Stats().SamplesDropped; len(dropped) != 0 {
			t.Errorf("SamplesDropped = %v, want none", dropped)
		}
	})

	t.Run("old records survive a blocked checkpoint correction and an unclean exit", func(t *testing.T) {
		dir := setup(t)
		// A directory where the checkpoint's temporary file goes blocks the write
		// that corrects the checkpoint at Open.
		blocker := filepath.Join(dir, ".acked.tmp")
		if err := os.Mkdir(blocker, 0o755); err != nil {
			t.Fatalf("block checkpoint write: %v", err)
		}
		var buf *Buffer
		var err error
		stderr := captureStderr(t, func() {
			buf, err = Open(dir, Options{Clock: clock.System()})
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !strings.Contains(stderr, "cannot save the corrected acked checkpoint") {
			t.Errorf("Open printed %q, want a warning about the failed save", stderr)
		}
		// The new records fill the rest of segment 1 and roll into segments numbered
		// past the checkpoint.
		for i := 0; i < 200; i++ {
			rec := make([]byte, recSize)
			copy(rec, fmt.Sprintf("new-%03d:", i))
			if err := buf.Append(rec); err != nil {
				t.Fatalf("Append new %d: %v", i, err)
			}
		}
		// No Close: the next Open finds whatever Open left on disk.
		if err := os.Remove(blocker); err != nil {
			t.Fatalf("unblock checkpoint write: %v", err)
		}
		reopened, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		defer func() { _ = reopened.Close() }()
		got := drainAndAck(t, reopened)
		if len(got) != 400 {
			t.Fatalf("delivered %d records, want 400 (200 old, then 200 new)", len(got))
		}
		for i := range got {
			prefix := fmt.Sprintf("old-%03d:", i)
			if i >= 200 {
				prefix = fmt.Sprintf("new-%03d:", i-200)
			}
			if !strings.HasPrefix(got[i], prefix) {
				t.Fatalf("record %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
			}
		}
		if dropped := reopened.Stats().SamplesDropped; len(dropped) != 0 {
			t.Errorf("SamplesDropped = %v, want none", dropped)
		}
	})

	t.Run("open refuses when the stale checkpoint can be neither saved nor removed", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("a read-only directory does not stop root")
		}
		dir := setup(t)
		// A read-only directory refuses both the write of the corrected checkpoint
		// and the removal of the stale one. Open must refuse rather than keep the
		// stale checkpoint and delete the old records unread on the next start.
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer func() { _ = os.Chmod(dir, 0o755) }() // let t.TempDir() clean up
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err == nil {
			_ = buf.Close()
			t.Fatal("Open succeeded, want an error: the stale checkpoint is still on disk")
		}
		if !strings.Contains(err.Error(), "stale acked checkpoint") {
			t.Errorf("Open error = %v, want one naming the stale acked checkpoint", err)
		}
	})
}

// TestBuffer_CheckpointPastSegmentEndRestartsAtFileEnd — AB-16: a power loss can
// leave a segment shorter than the checkpoint that names it, because the
// checkpoint is renamed into place without a sync and the segment data is not.
// The checkpoint then points past the end of the file, and the appends that
// follow land below it, where the reader never looks.
func TestBuffer_CheckpointPastSegmentEndRestartsAtFileEnd(t *testing.T) {
	// Three records, the first two acknowledged, so the checkpoint is at byte 32.
	// The segment then loses its tail and keeps only record-0.
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		for _, r := range []string{"record-0", "record-1", "record-2"} {
			if err := buf.Append([]byte(r)); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
		for i := 0; i < 2; i++ {
			data, ack, err := buf.Next()
			if err != nil || data == nil {
				t.Fatalf("Next %d: %q, %v", i, data, err)
			}
			if err := ack(context.Background()); err != nil {
				t.Fatalf("ack %d: %v", i, err)
			}
		}
		if err := buf.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := os.Truncate(firstSegmentFile(t, dir), 16); err != nil {
			t.Fatalf("Truncate: %v", err)
		}
		return dir
	}

	t.Run("appends after the reopen are delivered", func(t *testing.T) {
		dir := setup(t)
		var buf *Buffer
		var err error
		stderr := captureStderr(t, func() {
			buf, err = Open(dir, Options{Clock: clock.System()})
		})
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer func() { _ = buf.Close() }()
		if !strings.Contains(stderr, "past the end") {
			t.Errorf("reopen printed %q, want a warning about the checkpoint", stderr)
		}
		if err := buf.Append([]byte("record-3")); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if got := drainAndAck(t, buf); len(got) != 1 || got[0] != "record-3" {
			t.Fatalf("delivered %q, want [record-3]", got)
		}
	})

	t.Run("appends survive an unclean exit before any ack", func(t *testing.T) {
		dir := setup(t)
		buf, err := Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		if err := buf.Append([]byte("record-3")); err != nil {
			t.Fatalf("Append: %v", err)
		}
		// No Close: the checkpoint on disk is the one Open wrote.
		buf, err = Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		defer func() { _ = buf.Close() }()
		if got := drainAndAck(t, buf); len(got) != 1 || got[0] != "record-3" {
			t.Fatalf("delivered %q, want [record-3]", got)
		}
	})

	t.Run("appends survive an unclean exit when the correction cannot be saved", func(t *testing.T) {
		dir := setup(t)
		blocker := filepath.Join(dir, ".acked.tmp")
		if err := os.Mkdir(blocker, 0o755); err != nil {
			t.Fatalf("block checkpoint write: %v", err)
		}
		var buf *Buffer
		var err error
		stderr := captureStderr(t, func() {
			buf, err = Open(dir, Options{Clock: clock.System()})
		})
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		if !strings.Contains(stderr, "cannot save the corrected acked checkpoint") {
			t.Errorf("reopen printed %q, want a warning about the failed save", stderr)
		}
		if err := buf.Append([]byte("record-3")); err != nil {
			t.Fatalf("Append: %v", err)
		}
		// No Close: the next Open finds whatever Open left on disk.
		if err := os.Remove(blocker); err != nil {
			t.Fatalf("unblock checkpoint write: %v", err)
		}
		buf, err = Open(dir, Options{Clock: clock.System()})
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		defer func() { _ = buf.Close() }()
		// record-0 was acked before the cut. The stale checkpoint is removed rather
		// than kept, so the next Open replays from the first segment and offers it
		// again. That repeats a delivered record, which at-least-once allows; the
		// record-3 appended after the cut must not be lost.
		if got := drainAndAck(t, buf); len(got) != 2 || got[0] != "record-0" || got[1] != "record-3" {
			t.Fatalf("delivered %q, want [record-0 record-3]", got)
		}
	})
}

// frameRecord returns payload framed the way Segment.Append writes it: the
// length, the CRC32C of the payload, then the payload.
func frameRecord(payload string) []byte {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(buf[4:8], computeCRC32C([]byte(payload)))
	copy(buf[8:], payload)
	return buf
}

// TestBuffer_StaleCheckpointSurvivesFailedSave — AB-03: the corrected checkpoint
// cannot be written, so the stale one is removed rather than left to make the
// records written after the cleanup look acknowledged. The next Open finds no
// checkpoint and must deliver all of them.
func TestBuffer_StaleCheckpointSurvivesFailedSave(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// 400 records of this size span four segments; draining them all leaves
	// the checkpoint in the last one.
	const recSize = 64 * 1024
	for i := 0; i < 400; i++ {
		rec := make([]byte, recSize)
		copy(rec, fmt.Sprintf("old-%03d:", i))
		if err := buf.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if got := len(drainAndAck(t, buf)); got != 400 {
		t.Fatalf("drained %d old records, want 400", got)
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	segs, err := filepath.Glob(filepath.Join(dir, "*.seg"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("no segment files to remove: %v", err)
	}
	for _, s := range segs {
		if err := os.Remove(s); err != nil {
			t.Fatalf("remove %s: %v", s, err)
		}
	}

	// A directory where the checkpoint's temporary file goes makes the write fail.
	blocker := filepath.Join(dir, ".acked.tmp")
	if err := os.Mkdir(blocker, 0o755); err != nil {
		t.Fatalf("block checkpoint write: %v", err)
	}
	var reopened *Buffer
	stderr := captureStderr(t, func() {
		reopened, err = Open(dir, Options{Clock: clock.System()})
	})
	if err != nil {
		t.Fatalf("Open after cleanup: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if !strings.Contains(stderr, "cannot save the corrected acked checkpoint") {
		t.Errorf("Open printed %q, want a warning about the failed save", stderr)
	}
	// 400 fresh records reach the checkpoint's own segment ID and past it.
	for i := 0; i < 400; i++ {
		rec := make([]byte, recSize)
		copy(rec, fmt.Sprintf("new-%03d:", i))
		if err := reopened.Append(rec); err != nil {
			t.Fatalf("Append new %d: %v", i, err)
		}
	}
	// No Close: the stale checkpoint is what the next Open finds.
	if err := os.Remove(blocker); err != nil {
		t.Fatalf("unblock checkpoint write: %v", err)
	}
	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = buf.Close() }()
	got := drainAndAck(t, buf)
	if len(got) != 400 {
		t.Fatalf("delivered %d records written after the cleanup, want 400", len(got))
	}
	for i := range got {
		if prefix := fmt.Sprintf("new-%03d:", i); !strings.HasPrefix(got[i], prefix) {
			t.Fatalf("record %d starts %q, want %q", i, got[i][:min(len(got[i]), len(prefix))], prefix)
		}
	}
}

// TestBuffer_ZeroFilledTailIsNotDelivered — AB-13: a zero-filled tail, which
// some filesystems leave after a crash, is not a record. Its zero headers carry
// the CRC of empty input, which is 0, so they used to pass the check and reach
// the pusher as empty envelopes, to be acked without a trace.
func TestBuffer_ZeroFilledTailIsNotDelivered(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := buf.Append([]byte("hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	segPath := firstSegmentFile(t, dir)
	f, err := os.OpenFile(segPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := f.Write(make([]byte, 24)); err != nil {
		t.Fatalf("write zero tail: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	buf, err = Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = buf.Close() }()
	// The zeros are cut off on open, so a retired segment holds no garbage that
	// could block its removal once every record in it is acked.
	info, err := os.Stat(segPath)
	if err != nil {
		t.Fatalf("stat segment: %v", err)
	}
	if want := int64(len(frameRecord("hello"))); info.Size() != want {
		t.Errorf("segment is %d bytes after reopen, want %d: the zero tail was not cut", info.Size(), want)
	}
	if got := drainAndAck(t, buf); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("delivered %q, want [hello]", got)
	}
	if n := buf.Stats().CorruptRecords; n == 0 {
		t.Error("the zero-filled tail was not counted as corruption")
	}
}

// TestBuffer_ZeroLengthHeaderInsideSegmentIsSkipped — AB-13: a zero-length
// header between two records is skipped as corrupt. It used to come back as an
// empty envelope.
func TestBuffer_ZeroLengthHeaderInsideSegmentIsSkipped(t *testing.T) {
	dir := t.TempDir()
	var seg []byte
	seg = append(seg, frameRecord("first")...)
	seg = append(seg, make([]byte, 8)...)
	seg = append(seg, frameRecord("second")...)
	if err := os.WriteFile(filepath.Join(dir, "00000000.seg"), seg, 0o644); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()
	if got := drainAndAck(t, buf); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("delivered %q, want [first second]", got)
	}
	if n := buf.Stats().CorruptRecords; n == 0 {
		t.Error("the zero-length header was not counted as corruption")
	}
}

// TestBuffer_AppendRejectsEmptyRecord — AB-13: an empty record is refused
// before it is written. The reader and the reopen scan both treat a
// zero-length header as no record, so one written here would vanish unseen.
func TestBuffer_AppendRejectsEmptyRecord(t *testing.T) {
	buf, err := Open(t.TempDir(), Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = buf.Close() }()

	if err := buf.Append(nil); err == nil {
		t.Error("Append(nil) succeeded")
	}
	if err := buf.Append([]byte{}); err == nil {
		t.Error("Append of an empty slice succeeded")
	}
	if got := drainAndAck(t, buf); len(got) != 0 {
		t.Fatalf("delivered %q after refused appends, want none", got)
	}
}

// TestBuffer_UnreadableCheckpointIsReported — AB-11: a checkpoint that exists
// but cannot be read still restarts from the first record, as a missing one
// does, and the fallback is reported. It used to be silent.
func TestBuffer_UnreadableCheckpointIsReported(t *testing.T) {
	dir := t.TempDir()
	buf, err := Open(dir, Options{Clock: clock.System()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, r := range []string{"acked", "pending"} {
		if err := buf.Append([]byte(r)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if _, ack, err := buf.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	} else if err := ack(context.Background()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A power loss can leave the checkpoint short: three bytes, not twelve.
	if err := os.WriteFile(filepath.Join(dir, ".acked"), []byte{1, 0, 0}, 0o644); err != nil {
		t.Fatalf("truncate checkpoint: %v", err)
	}

	var reopened *Buffer
	stderr := captureStderr(t, func() {
		reopened, err = Open(dir, Options{Clock: clock.System()})
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if !strings.Contains(stderr, "unreadable acked checkpoint") {
		t.Errorf("reopen with a short checkpoint printed %q, want a warning", stderr)
	}
	if got := drainAndAck(t, reopened); len(got) != 2 {
		t.Fatalf("delivered %q after a short checkpoint, want both records replayed", got)
	}
}

// TestBuffer_MissingCheckpointIsNotReported — AB-11: a buffer that has never
// acked anything has no checkpoint file. That is the normal first start, so it
// prints nothing.
func TestBuffer_MissingCheckpointIsNotReported(t *testing.T) {
	stderr := captureStderr(t, func() {
		buf, err := Open(t.TempDir(), Options{Clock: clock.System()})
		if err != nil {
			t.Errorf("Open: %v", err)
			return
		}
		_ = buf.Close()
	})
	if stderr != "" {
		t.Errorf("first start printed %q, want nothing", stderr)
	}
}

// captureStderr runs fn and returns what it wrote to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = saved }()

	done := make(chan string)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()
	fn()
	_ = w.Close()
	os.Stderr = saved
	return <-done
}
