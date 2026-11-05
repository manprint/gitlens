package indexadvice

import "math"

const (
	tupleOverheadBytes = 8 + 4
	sizeFactor         = 1.15
	assumedWidth       = 32
)

// btreeType reports whether a column type can be a btree key. An empty type
// (not reported) is accepted.
func btreeType(typ string) bool {
	switch typ {
	case "json", "xml", "point", "box", "circle", "line", "lseg", "path", "polygon", "tsvector", "tsquery":
		return false
	}
	return true
}

// estimateSize is reltuples × (Σ width + 8 + 4) × 1.15, a labelled estimate.
// Width is avg_width, else a fixed typlen, else 32 (note width_assumed). The
// result is nil, with note size_unknown, when the row count is unknown.
func estimateSize(meta TableMeta, hasMeta bool, cols []KeyColumn) (*int64, []string) {
	rows := meta.RelTuples
	if rows <= 0 {
		rows = meta.LiveTuples
	}
	if !hasMeta || rows <= 0 {
		return nil, []string{NoteSizeUnknown}
	}
	var notes []string
	width := 0.0
	for _, c := range cols {
		col := meta.Columns[c.Name]
		switch {
		case col.AvgWidth > 0:
			width += col.AvgWidth
		case col.TypLen > 0:
			width += float64(col.TypLen)
		default:
			width += assumedWidth
			if len(notes) == 0 {
				notes = append(notes, NoteWidthAssumed)
			}
		}
	}
	size := int64(math.Round(rows * (width + tupleOverheadBytes) * sizeFactor))
	return &size, notes
}
