package check

import (
	"context"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&sequencesCheck{}) }

// sequenceFactKind is the fact kind of one sequence. The server replaces a
// database's facts of the kind on every complete result, so a dropped
// sequence stops being reported.
const sequenceFactKind = "sequence"

// sequenceScanLimit bounds the sequences of one database the check looks at,
// those with the fewest values between their start and their limit first.
const sequenceScanLimit = 2000

// sequenceFactLimit bounds the sequences of one database the check reports,
// the closest to their limit first.
const sequenceFactLimit = 500

// sequenceValueBatch is how many sequences one statement reads the last value
// of. Reading a sequence locks it until the transaction ends, and every
// statement here is its own transaction: batches keep the locks one session
// holds at once well below the shared lock table's size.
const sequenceValueBatch = 100

// sequenceAtRiskRatio is the used fraction from which a sequence counts as at
// risk in the metrics; the advisor rule uses the same threshold for its
// warning.
const sequenceAtRiskRatio = 0.75

// sequencesQuery lists the sequences of the database with the column each
// feeds. A sequence is linked to a column by OWNED BY or an identity column
// (the sequence depends on the column) or by a column default that names it
// (the default depends on the sequence). When it feeds several columns, the
// one with the narrowest integer type is reported, as it overflows first.
// readable says whether the role may read the sequence's value: without
// SELECT or USAGE on it the value is hidden, and an unlogged sequence has no
// value on a standby. Only the catalogs are read here.
const sequencesQuery = `WITH links AS (
    SELECT d.objid AS seqid, d.refobjid AS relid, d.refobjsubid AS attnum,
           CASE d.deptype WHEN 'i' THEN 1 ELSE 2 END AS rank
      FROM pg_catalog.pg_depend d
      JOIN pg_catalog.pg_sequence ps ON ps.seqrelid = d.objid
     WHERE d.classid = 'pg_catalog.pg_class'::pg_catalog.regclass
       AND d.refclassid = 'pg_catalog.pg_class'::pg_catalog.regclass
       AND d.deptype IN ('a', 'i')
       AND d.refobjsubid > 0
    UNION ALL
    SELECT d.refobjid, ad.adrelid, ad.adnum, 3
      FROM pg_catalog.pg_depend d
      JOIN pg_catalog.pg_sequence ps ON ps.seqrelid = d.refobjid
      JOIN pg_catalog.pg_attrdef ad ON ad.oid = d.objid
     WHERE d.classid = 'pg_catalog.pg_attrdef'::pg_catalog.regclass
       AND d.refclassid = 'pg_catalog.pg_class'::pg_catalog.regclass
  ), cols AS (
    SELECT l.seqid, l.relid, l.attnum, l.rank, tn.nspname, tc.relname, a.attname,
           pg_catalog.format_type(a.atttypid, a.atttypmod) AS typname,
           tc.relispartition, GREATEST(tc.reltuples, 0)::pg_catalog.float8 AS reltuples,
           CASE COALESCE(NULLIF(t.typbasetype, 0), t.oid)
                WHEN 'pg_catalog.int2'::pg_catalog.regtype THEN 32767::pg_catalog.int8
                WHEN 'pg_catalog.int4'::pg_catalog.regtype THEN 2147483647::pg_catalog.int8
                WHEN 'pg_catalog.int8'::pg_catalog.regtype THEN 9223372036854775807::pg_catalog.int8 END AS type_max,
           CASE COALESCE(NULLIF(t.typbasetype, 0), t.oid)
                WHEN 'pg_catalog.int2'::pg_catalog.regtype THEN (-32768)::pg_catalog.int8
                WHEN 'pg_catalog.int4'::pg_catalog.regtype THEN (-2147483648)::pg_catalog.int8
                WHEN 'pg_catalog.int8'::pg_catalog.regtype THEN (-9223372036854775808)::pg_catalog.int8 END AS type_min
      FROM links l
      JOIN pg_catalog.pg_class tc ON tc.oid = l.relid
      JOIN pg_catalog.pg_namespace tn ON tn.oid = tc.relnamespace
      JOIN pg_catalog.pg_attribute a ON a.attrelid = l.relid AND a.attnum = l.attnum AND NOT a.attisdropped
      JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
  )
SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname),
       c.oid, n.nspname, c.relname, pg_catalog.format_type(s.seqtypid, NULL),
       s.seqstart, s.seqincrement, s.seqmin, s.seqmax, s.seqcycle,
       pg_catalog.has_sequence_privilege(c.oid, 'SELECT, USAGE')
         AND NOT (c.relpersistence = 'u' AND pg_catalog.pg_is_in_recovery()),
       COALESCE(col.nspname, ''), COALESCE(col.relname, ''), COALESCE(col.attname, ''),
       COALESCE(col.typname, ''), COALESCE(col.rank, 0), col.type_max, col.type_min,
       (SELECT pg_catalog.count(DISTINCT (x.relid, x.attnum)) FROM cols x WHERE x.seqid = c.oid)
  FROM pg_catalog.pg_sequence s
  JOIN pg_catalog.pg_class c ON c.oid = s.seqrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  LEFT JOIN LATERAL (
       SELECT x.*
         FROM cols x
        WHERE x.seqid = c.oid
        ORDER BY x.type_max NULLS LAST, x.relispartition, x.rank, x.nspname, x.relname, x.attname
        LIMIT 1) col ON true
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
 ORDER BY CASE WHEN s.seqincrement > 0
               THEN LEAST(s.seqmax, COALESCE(col.type_max, s.seqmax))::pg_catalog.numeric - s.seqstart
               ELSE s.seqstart - GREATEST(s.seqmin, COALESCE(col.type_min, s.seqmin))::pg_catalog.numeric END,
          col.reltuples DESC NULLS LAST, 1
 LIMIT $1`

// sequenceValuesQuery reads the last value of each sequence of $1 the role
// may still read: NULL for a sequence never called, or one dropped or no
// longer granted since sequencesQuery. pg_sequence_last_value only reads; it
// never advances the sequence.
const sequenceValuesQuery = `SELECT x.oid,
       CASE WHEN pg_catalog.has_sequence_privilege(x.oid, 'SELECT, USAGE')
            THEN pg_catalog.pg_sequence_last_value(x.oid::pg_catalog.regclass) END
  FROM pg_catalog.unnest($1::pg_catalog.oid[]) AS x (oid)`

// sequencesCheck reports the sequences of each database: how much of its
// range each has used, counted against the narrowest of its own limit and
// the integer type of the column it feeds, and the sequences whose column
// overflows before the sequence would.
type sequencesCheck struct{}

func (c *sequencesCheck) Name() string { return "sequences" }
func (c *sequencesCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *sequencesCheck) DefaultInterval() time.Duration { return 10 * time.Minute }
func (c *sequencesCheck) Timeout() time.Duration         { return 30 * time.Second }

type sequenceRow struct {
	key, schema, name, dataType           string
	oid                                   uint32
	start, increment, min, max            int64
	cycle, readable                       bool
	colSchema, colTable, colName, colType string
	linkRank                              int32
	typeMax, typeMin                      *int64
	columns                               int64

	// read is set once sequenceValuesQuery answered for the sequence, and
	// lastValue then holds its value, nil when it was never called.
	read      bool
	lastValue *int64
	usage     sequenceUsage
}

func (c *sequencesCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, sequencesQuery, sequenceScanLimit+1)
	if err != nil {
		return Result{}, err
	}
	var seqs []*sequenceRow
	var result Result
	for rows.Next() {
		s := &sequenceRow{}
		if err := rows.Scan(&s.key, &s.oid, &s.schema, &s.name, &s.dataType,
			&s.start, &s.increment, &s.min, &s.max, &s.cycle, &s.readable,
			&s.colSchema, &s.colTable, &s.colName, &s.colType, &s.linkRank, &s.typeMax, &s.typeMin,
			&s.columns); err != nil {
			rows.Close()
			return Result{}, err
		}
		if len(seqs) == sequenceScanLimit {
			result.Truncated = true
			break
		}
		seqs = append(seqs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	partial, err := c.readValues(ctx, conn, seqs)
	if err != nil {
		return Result{}, err
	}
	result.Partial = partial

	for _, s := range seqs {
		s.usage = sequenceUsageOf(s)
	}
	sort.SliceStable(seqs, func(i, j int) bool { return seqs[i].usage.before(seqs[j].usage) })
	if len(seqs) > sequenceFactLimit {
		result.Partial = true
	}

	var hidden, mismatched, atRisk int
	maxRatio := -1.0
	for i, s := range seqs {
		u := s.usage
		if u.state == "hidden" {
			hidden++
		}
		if u.columnBinds {
			mismatched++
		}
		if u.ratio != nil {
			maxRatio = max(maxRatio, *u.ratio)
			if *u.ratio >= sequenceAtRiskRatio && u.exhausts {
				atRisk++
			}
		}
		if i < sequenceFactLimit {
			result.Facts = append(result.Facts, sequenceFact(s))
		}
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_sequences", Value: float64(len(seqs)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_sequences_value_hidden", Value: float64(hidden), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_sequences_column_narrower", Value: float64(mismatched), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_sequences_at_risk", Value: float64(atRisk), Kind: pgtype.KindGauge},
	)
	if maxRatio >= 0 {
		result.Metrics = append(result.Metrics,
			pgtype.Metric{Name: "pg_sequence_max_used_ratio", Value: maxRatio, Kind: pgtype.KindGauge})
	}
	return result, nil
}

// readValues reads the last value of the readable sequences, a batch per
// statement. A batch that waits too long for a lock (ALTER SEQUENCE holds
// one) leaves its sequences unread and the result partial; any other error
// fails the check.
func (c *sequencesCheck) readValues(ctx context.Context, conn Conn, seqs []*sequenceRow) (partial bool, err error) {
	byOID := map[uint32]*sequenceRow{}
	var oids []uint32
	for _, s := range seqs {
		if s.readable {
			byOID[s.oid] = s
			oids = append(oids, s.oid)
		}
	}
	for start := 0; start < len(oids); start += sequenceValueBatch {
		batch := oids[start:min(start+sequenceValueBatch, len(oids))]
		rows, err := conn.Query(ctx, sequenceValuesQuery, batch)
		if err == nil {
			err = scanSequenceValues(rows, byOID)
		}
		if pgErrorCode(err) == pgLockNotAvailable {
			partial = true
			continue
		}
		if err != nil {
			return false, err
		}
	}
	return partial, nil
}

func scanSequenceValues(rows pgx.Rows, byOID map[uint32]*sequenceRow) error {
	defer rows.Close()
	for rows.Next() {
		var oid uint32
		var value *int64
		if err := rows.Scan(&oid, &value); err != nil {
			return err
		}
		if s := byOID[oid]; s != nil {
			s.read, s.lastValue = true, value
		}
	}
	return rows.Err()
}

// sequenceUsage is how far a sequence is through its range.
type sequenceUsage struct {
	// state is "used" (the value is known), "unused" (never called),
	// "hidden" (the role may not read the value) or "unread" (the read
	// waited too long for a lock).
	state string
	// limit is the last value the sequence can hand out that its column
	// accepts: the nearer of the sequence's own bound and the bound of the
	// column's integer type.
	limit int64
	// columnBinds is set when the column's type, not the sequence, sets the
	// limit: the column overflows before the sequence runs out.
	columnBinds bool
	// exhausts is false for a cycling sequence whose own bound sets the
	// limit: it wraps around instead of failing.
	exhausts bool
	// ratio is the used fraction of the range from start to limit, and
	// remaining the values left, when the value is known.
	ratio     *float64
	remaining *big.Int
}

// before orders sequences by risk: the highest known used fraction first,
// then the sequences whose column binds, then by key.
func (u sequenceUsage) before(v sequenceUsage) bool {
	ur, vr := -1.0, -1.0
	if u.ratio != nil {
		ur = *u.ratio
	}
	if v.ratio != nil {
		vr = *v.ratio
	}
	if ur != vr {
		return ur > vr
	}
	return u.columnBinds && !v.columnBinds
}

func sequenceUsageOf(s *sequenceRow) sequenceUsage {
	ascending := s.increment > 0
	u := sequenceUsage{limit: s.max}
	if !ascending {
		u.limit = s.min
	}
	switch {
	case ascending && s.typeMax != nil && *s.typeMax < s.max:
		u.limit, u.columnBinds = *s.typeMax, true
	case !ascending && s.typeMin != nil && *s.typeMin > s.min:
		u.limit, u.columnBinds = *s.typeMin, true
	}
	u.exhausts = !s.cycle || u.columnBinds

	switch {
	case !s.readable:
		u.state = "hidden"
		return u
	case !s.read:
		u.state = "unread"
		return u
	case s.lastValue == nil:
		u.state = "unused"
	default:
		u.state = "used"
	}

	// The arithmetic is done on big integers: the range of a bigint
	// sequence does not fit an int64.
	limit, start, step := big.NewInt(u.limit), big.NewInt(s.start), big.NewInt(s.increment)
	span := new(big.Int).Sub(limit, start)
	if !ascending {
		span.Neg(span)
		step.Neg(step)
	}
	var used, left *big.Int
	if s.lastValue == nil {
		// The first call returns start itself.
		used = big.NewInt(0)
		left = new(big.Int).Add(new(big.Int).Quo(span, step), big.NewInt(1))
	} else {
		last := big.NewInt(*s.lastValue)
		used = new(big.Int).Sub(last, start)
		left = new(big.Int).Sub(limit, last)
		if !ascending {
			used.Neg(used)
			left.Neg(left)
		}
		left.Quo(left, step)
	}
	if left.Sign() < 0 {
		left.SetInt64(0)
	}
	ratio := 1.0
	if span.Sign() > 0 {
		r, _ := new(big.Rat).SetFrac(used, span).Float64()
		ratio = min(max(r, 0), 1)
	}
	u.ratio, u.remaining = &ratio, left
	return u
}

func sequenceFact(s *sequenceRow) Fact {
	u := s.usage
	link := ""
	switch s.linkRank {
	case 1:
		link = "identity"
	case 2:
		link = "owned"
	case 3:
		link = "default"
	}
	limitSource := "sequence"
	if u.columnBinds {
		limitSource = "column"
	}
	labels := map[string]string{
		"schemaname": s.schema, "sequence": s.name, "data_type": s.dataType,
		"start": strconv.FormatInt(s.start, 10), "increment": strconv.FormatInt(s.increment, 10),
		"min": strconv.FormatInt(s.min, 10), "max": strconv.FormatInt(s.max, 10),
		"cycle":        strconv.FormatBool(s.cycle),
		"table_schema": s.colSchema, "table": s.colTable, "column": s.colName, "column_type": s.colType,
		"link": link, "columns": strconv.FormatInt(s.columns, 10),
		"limit": strconv.FormatInt(u.limit, 10), "limit_source": limitSource,
		"exhausts":   strconv.FormatBool(u.exhausts),
		"last_value": "", "used_ratio": "", "remaining": "",
	}
	if s.lastValue != nil {
		labels["last_value"] = strconv.FormatInt(*s.lastValue, 10)
	}
	if u.ratio != nil {
		labels["used_ratio"] = strconv.FormatFloat(*u.ratio, 'f', 6, 64)
		labels["remaining"] = u.remaining.String()
	}
	return Fact{Kind: sequenceFactKind, Key: s.key, ValueText: u.state, Labels: labels}
}
