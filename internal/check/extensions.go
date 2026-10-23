package check

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&extensionsCheck{}) }

// extensionFactKind is the fact kind of one installed extension. The server
// replaces a database's extension facts on every complete result, so a
// dropped extension stops being reported.
const extensionFactKind = "extension"

// extensionFactLimit bounds the extensions of one database.
const extensionFactLimit = 200

// extensionsQuery lists the extensions installed in the database with the
// version the server's installed packages would update them to.
// pg_available_extensions() reads every control file of the installation,
// which takes tens to hundreds of milliseconds, so the check runs hourly. It
// only reads: updating an extension is ALTER EXTENSION, which pglens never
// runs.
const extensionsQuery = `SELECT e.extname, n.nspname, e.extversion, COALESCE(a.default_version, '')
  FROM pg_catalog.pg_extension e
  JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
  LEFT JOIN pg_catalog.pg_available_extensions() AS a (name, default_version, comment) ON a.name = e.extname
 ORDER BY e.extname
 LIMIT $1`

// extensionsCheck reports the extensions of each database, their versions,
// and whether the installed packages carry a newer one.
type extensionsCheck struct{}

func (c *extensionsCheck) Name() string { return "extensions" }
func (c *extensionsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *extensionsCheck) DefaultInterval() time.Duration { return time.Hour }
func (c *extensionsCheck) Timeout() time.Duration         { return 15 * time.Second }

func (c *extensionsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, extensionsQuery, extensionFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	upgradable := 0
	for rows.Next() {
		var name, schema, version, available string
		if err := rows.Scan(&name, &schema, &version, &available); err != nil {
			return Result{}, err
		}
		if len(result.Facts) == extensionFactLimit {
			result.Truncated = true
			break
		}
		upgrade := extensionUpgrade(version, available)
		if upgrade == "available" {
			upgradable++
		}
		result.Facts = append(result.Facts, Fact{
			Kind: extensionFactKind, Key: name, ValueText: version,
			Labels: map[string]string{"schema": schema, "default_version": available, "upgrade": upgrade},
		})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_extensions_installed", Value: float64(len(result.Facts)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_extensions_upgrade_available", Value: float64(upgradable), Kind: pgtype.KindGauge},
	)
	return result, nil
}

// extensionUpgrade compares an installed extension version with the default
// version of the installed packages: "available" when the default is newer,
// "none" when it is the same or older, "unknown" when the packages no longer
// carry the extension or a version is not dotted numbers ("1.5beta").
func extensionUpgrade(installed, available string) string {
	if available == "" {
		return "unknown"
	}
	if installed == available {
		return "none"
	}
	a, okA := dottedVersion(available)
	b, okB := dottedVersion(installed)
	if !okA || !okB {
		return "unknown"
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return "available"
			}
			return "none"
		}
	}
	return "none"
}

func dottedVersion(v string) ([]int, bool) {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
