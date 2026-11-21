package pglog

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
)

// Log formats pglog reads.
const (
	FormatJSON = "jsonlog"
	FormatCSV  = "csvlog"
)

// Records are the complete records Split found: JSON lines for jsonlog, field
// lists for csvlog. Bad counts the csvlog records that could not be read and
// were skipped.
type Records struct {
	JSON [][]byte
	CSV  [][]string
	Bad  int
}

// Len is the number of records found.
func (r Records) Len() int { return len(r.JSON) + len(r.CSV) }

// Split returns the complete records at the start of data and the bytes they
// use. A record still being written (no final newline, or a csvlog quote still
// open at the end) stays unread, so the next read starts at it. A record longer
// than maxLine is cut to maxLine (I-9).
func Split(format string, data []byte, maxLine int) (Records, int) {
	if maxLine <= 0 {
		maxLine = MaxLineBytes
	}
	if format == FormatCSV {
		return splitCSV(data, maxLine)
	}
	return splitJSON(data, maxLine)
}

func splitJSON(data []byte, maxLine int) (Records, int) {
	var out Records
	consumed := 0
	for consumed < len(data) {
		rest := data[consumed:]
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			// A partial line longer than the cut is shipped cut and its bytes
			// consumed; anything shorter waits for its newline.
			if len(rest) > maxLine {
				out.JSON = append(out.JSON, rest[:maxLine])
				consumed = len(data)
			}
			break
		}
		line := bytes.TrimRight(rest[:end], "\r")
		consumed += end + 1
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(line) > maxLine {
			line = line[:maxLine]
		}
		out.JSON = append(out.JSON, line)
	}
	return out, consumed
}

func splitCSV(data []byte, maxLine int) (Records, int) {
	var out Records
	base, done := 0, 0 // base: where the current reader starts; done: bytes of whole records
	for base < len(data) {
		r := csv.NewReader(bytes.NewReader(data[base:]))
		r.FieldsPerRecord = -1
		restart := -1
		for restart < 0 {
			start := base + int(r.InputOffset())
			fields, err := r.Read()
			if errors.Is(err, io.EOF) {
				return out, done
			}
			end := base + int(r.InputOffset())
			if err != nil {
				if end >= len(data) {
					// The record runs to the end of the data: still being written.
					return out, done
				}
				// A broken record in the middle: skip to the next line start.
				out.Bad++
				nl := bytes.IndexByte(data[start:], '\n')
				if nl < 0 {
					return out, done
				}
				restart = start + nl + 1
				continue
			}
			if data[end-1] != '\n' {
				// The last record has no newline yet.
				return out, done
			}
			for i, f := range fields {
				if len(f) > maxLine {
					fields[i] = f[:maxLine]
				}
			}
			out.CSV = append(out.CSV, fields)
			done = end
		}
		base, done = restart, restart
	}
	return out, done
}
