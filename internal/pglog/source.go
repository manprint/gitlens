package pglog

import (
	"context"
	"errors"
)

// Source reads the server's log files.
type Source interface {
	// Current names the file the server writes now and gives its size.
	Current(ctx context.Context) (name string, size int64, err error)
	// Size gives the size of a log file; false when it is gone.
	Size(ctx context.Context, name string) (int64, bool, error)
	// ReadAt reads up to n bytes of a log file from off.
	ReadAt(ctx context.Context, name string, off, n int64) ([]byte, error)
}

var (
	// ErrNoLogFile means the server writes no structured log file: the
	// collector is off or the format is not in log_destination.
	ErrNoLogFile = errors.New("pglog: no structured log file")
	// ErrLogReadDenied means the role may not read the log files.
	ErrLogReadDenied = errors.New("pglog: log read denied")
	// ErrBadLogName means a file name outside the log directory.
	ErrBadLogName = errors.New("pglog: log file name outside the log directory")
)

// BacklogFactor bounds the unread bytes kept: a larger backlog than
// BacklogFactor × budget is skipped (I-9).
const BacklogFactor = 16

// Position is where reading resumes in a target's log.
type Position struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

// Chunk is bytes read from one log file starting at Offset. Ended marks a
// file read to its end after the server moved to another one: a partial
// record at its end will never complete.
type Chunk struct {
	File   string
	Offset int64
	Data   []byte
	Ended  bool
}

// Settle gives the position after the caller used consumed bytes of the
// last chunk: a partial record left in a file still being written is read
// again next time.
func Settle(next Position, last Chunk, consumed int) Position {
	if last.Ended || last.File != next.File {
		return next
	}
	return Position{File: last.File, Offset: last.Offset + int64(consumed)}
}

// Advance reads what was written since pos, at most budget bytes. On the
// first run it starts at the end of the current file. A file shorter than
// the offset was truncated and is read from 0. When the server moved to a
// new file, the rest of the old one is read first, then the new one from 0.
// A backlog over BacklogFactor × budget is skipped: next jumps to the end of
// the current file and skipped counts the bytes passed over.
func Advance(ctx context.Context, src Source, pos Position, first bool, budget int64) ([]Chunk, Position, int64, error) {
	name, size, err := src.Current(ctx)
	if err != nil {
		return nil, pos, 0, err
	}
	end := Position{File: name, Offset: size}
	if first || pos.File == "" {
		return nil, end, 0, nil
	}
	type span struct {
		file     string
		off, len int64
		ended    bool
	}
	var spans []span
	if pos.File == name {
		off := pos.Offset
		if size < off {
			off = 0
		}
		spans = append(spans, span{name, off, size - off, false})
	} else {
		oldSize, ok, err := src.Size(ctx, pos.File)
		if err != nil {
			return nil, pos, 0, err
		}
		if ok && oldSize > pos.Offset {
			spans = append(spans, span{pos.File, pos.Offset, oldSize - pos.Offset, true})
		}
		spans = append(spans, span{name, 0, size, false})
	}
	var backlog int64
	for _, s := range spans {
		backlog += s.len
	}
	if backlog > BacklogFactor*budget {
		return nil, end, backlog, nil
	}
	var chunks []Chunk
	next := end
	left := budget
	for _, s := range spans {
		if s.len == 0 {
			continue
		}
		n := min(s.len, left)
		if n == 0 {
			next = Position{File: s.file, Offset: s.off}
			break
		}
		data, err := src.ReadAt(ctx, s.file, s.off, n)
		if err != nil {
			return chunks, Position{File: s.file, Offset: s.off}, 0, err
		}
		got := int64(len(data))
		chunks = append(chunks, Chunk{File: s.file, Offset: s.off, Data: data, Ended: s.ended && got == s.len})
		left -= got
		if got < s.len {
			next = Position{File: s.file, Offset: s.off + got}
			break
		}
	}
	return chunks, next, 0, nil
}
