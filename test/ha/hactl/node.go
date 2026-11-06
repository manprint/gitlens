package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// nodeConfig names a PostgreSQL node. Name is the service name other nodes
// and primary_conninfo use (and the pglens target name); Addr is where hactl
// dials it; Slot is the physical slot the node streams from on the leader.
type nodeConfig struct {
	Name string
	Addr string
	Slot string
}

type probe struct {
	Reachable  bool
	InRecovery bool
	LSN        uint64
	ReceiveLSN uint64
	Timeline   int
	Upstream   string
}

type pgOps interface {
	Probe(ctx context.Context, n nodeConfig) (probe, error)
	Promote(ctx context.Context, n nodeConfig) error
	EnsureSlot(ctx context.Context, on nodeConfig, slot string) error
	Repoint(ctx context.Context, standby, leader nodeConfig) error
	Detach(ctx context.Context, standby nodeConfig) error
}

type pgxOps struct {
	user, password, dbname string
	replUser, replPassword string
	replPort               int
}

func (o pgxOps) connect(ctx context.Context, n nodeConfig) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(n.Addr)
	if err != nil {
		return nil, err
	}
	cfg.Host, cfg.Port = host, port
	cfg.User, cfg.Password, cfg.Database = o.user, o.password, o.dbname
	cfg.TLSConfig = nil
	cfg.Fallbacks = nil
	cfg.ConnectTimeout = 800 * time.Millisecond
	cfg.RuntimeParams = map[string]string{"application_name": "hactl"}
	return pgx.ConnectConfig(ctx, cfg)
}

// probeSQL: the timeline of a primary comes from its current WAL file name,
// because pg_control_checkpoint() lags until the first checkpoint after a
// promotion; a standby reports the timeline it receives, or its checkpoint
// timeline while no WAL receiver runs.
const probeSQL = `
SELECT r,
  CASE WHEN r THEN pg_wal_lsn_diff(coalesce(pg_last_wal_replay_lsn(), '0/0'), '0/0')
       ELSE pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') END::bigint,
  CASE WHEN r THEN pg_wal_lsn_diff(greatest(coalesce(pg_last_wal_receive_lsn(), '0/0'),
                                            coalesce(pg_last_wal_replay_lsn(), '0/0')), '0/0')
       ELSE pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') END::bigint,
  CASE WHEN r THEN coalesce((SELECT received_tli FROM pg_stat_wal_receiver),
                            (SELECT timeline_id FROM pg_control_checkpoint()))
       ELSE ('x' || substr(pg_walfile_name(pg_current_wal_lsn()), 1, 8))::bit(32)::int END,
  CASE WHEN r THEN current_setting('primary_conninfo') ELSE '' END
FROM (SELECT pg_is_in_recovery() AS r) s`

func (o pgxOps) Probe(ctx context.Context, n nodeConfig) (probe, error) {
	conn, err := o.connect(ctx, n)
	if err != nil {
		return probe{}, err
	}
	defer closeConn(ctx, conn)
	var p probe
	var lsn, rcv int64
	var conninfo string
	if err := conn.QueryRow(ctx, probeSQL).Scan(&p.InRecovery, &lsn, &rcv, &p.Timeline, &conninfo); err != nil {
		return probe{}, err
	}
	p.Reachable = true
	p.LSN, p.ReceiveLSN = uint64(lsn), uint64(rcv)
	p.Upstream = conninfoValue(conninfo, "host")
	return p, nil
}

func (o pgxOps) Promote(ctx context.Context, n nodeConfig) error {
	conn, err := o.connect(ctx, n)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_promote(true, 30)").Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("pg_promote on %s returned false", n.Name)
	}
	return nil
}

func (o pgxOps) EnsureSlot(ctx context.Context, on nodeConfig, slot string) error {
	conn, err := o.connect(ctx, on)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	// A slot invalidated by max_slot_wal_keep_size can never stream again.
	if _, err = conn.Exec(ctx, `SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots
WHERE slot_name = $1 AND wal_status = 'lost' AND NOT active`, slot); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `SELECT pg_create_physical_replication_slot($1)
WHERE NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)`, slot)
	return err
}

// Detach stops the standby's WAL receiver, so it cannot receive WAL past the
// fork point from a partitioned old primary while another node is promoted.
func (o pgxOps) Detach(ctx context.Context, standby nodeConfig) error {
	conn, err := o.connect(ctx, standby)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	for _, q := range []string{"ALTER SYSTEM SET primary_conninfo = ''", "SELECT pg_reload_conf()"} {
		if _, err := conn.Exec(ctx, q); err != nil {
			return err
		}
	}
	for range 30 {
		var k int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_stat_wal_receiver").Scan(&k); err != nil {
			return err
		}
		if k == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s: WAL receiver still running after detach", standby.Name)
}

func (o pgxOps) Repoint(ctx context.Context, standby, leader nodeConfig) error {
	conn, err := o.connect(ctx, standby)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	ci := primaryConninfo(leader.Name, o.replPort, o.replUser, o.replPassword, standby.Name)
	for _, q := range []string{
		"ALTER SYSTEM SET primary_conninfo = " + quoteLiteral(ci),
		"ALTER SYSTEM SET primary_slot_name = " + quoteLiteral(standby.Slot),
		"SELECT pg_reload_conf()",
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func closeConn(ctx context.Context, conn *pgx.Conn) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = conn.Close(cctx)
}

func primaryConninfo(host string, port int, user, password, appName string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s application_name=%s",
		conninfoQuote(host), port, conninfoQuote(user), conninfoQuote(password), conninfoQuote(appName))
}

func conninfoQuote(v string) string {
	if v != "" && !strings.ContainsAny(v, " '\\\t\n") {
		return v
	}
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

func quoteLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// conninfoValue returns one keyword's value from a libpq keyword/value string.
func conninfoValue(s, key string) string {
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' {
			i++
		}
		k := s[start:i]
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return ""
		}
		i++
		for i < len(s) && s[i] == ' ' {
			i++
		}
		var v strings.Builder
		if i < len(s) && s[i] == '\'' {
			i++
			for i < len(s) && s[i] != '\'' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				v.WriteByte(s[i])
				i++
			}
			i++
		} else {
			for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				v.WriteByte(s[i])
				i++
			}
		}
		if k == key {
			return v.String()
		}
	}
	return ""
}

func splitHostPort(addr string) (string, uint16, error) {
	i := strings.LastIndexByte(addr, ':')
	if i <= 0 {
		return addr, 5432, nil
	}
	var port uint16
	if _, err := fmt.Sscanf(addr[i+1:], "%d", &port); err != nil {
		return "", 0, fmt.Errorf("bad address %q", addr)
	}
	return addr[:i], port, nil
}

func formatLSN(v uint64) string {
	return fmt.Sprintf("%X/%X", v>>32, uint32(v))
}

func parseLSN(s string) (uint64, error) {
	var hi, lo uint32
	if _, err := fmt.Sscanf(s, "%X/%X", &hi, &lo); err != nil {
		return 0, fmt.Errorf("bad LSN %q", s)
	}
	return uint64(hi)<<32 | uint64(lo), nil
}
