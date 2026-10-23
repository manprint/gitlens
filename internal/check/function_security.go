package check

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&functionSecurityCheck{}) }

// securityDefinerFactKind is the fact kind of one SECURITY DEFINER function
// or procedure. The server replaces a database's facts of the kind on every
// complete result, so a dropped or changed function stops being reported.
const securityDefinerFactKind = "security_definer_function"

// securityDefinerFactLimit bounds the SECURITY DEFINER functions of one
// database, those without a pinned search_path first.
const securityDefinerFactLimit = 200

// securityDefinerQuery lists the SECURITY DEFINER functions and procedures
// of the user schemas, outside extensions, with their owner, whether the
// owner is a superuser, the search_path they pin (NULL when none) and
// whether PUBLIC may execute them. A NULL proacl is the default ACL, which
// grants EXECUTE to PUBLIC, so the ACL is expanded with its default.
// Reading the catalog takes no lock on a function, so a function held by
// ALTER FUNCTION in another session does not stop the check.
const securityDefinerQuery = `SELECT * FROM (
SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(p.proname)
         || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')' AS signature,
       n.nspname, p.proname, pg_catalog.pg_get_function_identity_arguments(p.oid),
       CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END,
       l.lanname, pg_catalog.pg_get_userbyid(p.proowner), COALESCE(r.rolsuper, false) AS superuser,
       (SELECT pg_catalog.substring(c, 13) FROM pg_catalog.unnest(p.proconfig) AS c
         WHERE c LIKE 'search\_path=%' LIMIT 1) AS search_path,
       EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(p.proacl, pg_catalog.acldefault('f', p.proowner))) a
                WHERE a.grantee = 0 AND a.privilege_type = 'EXECUTE') AS public_execute
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  JOIN pg_catalog.pg_language l ON l.oid = p.prolang
  LEFT JOIN pg_catalog.pg_roles r ON r.oid = p.proowner
 WHERE p.prosecdef AND p.prokind IN ('f', 'p')
   AND n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
   AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
                    WHERE d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
                      AND d.objid = p.oid AND d.deptype = 'e')
) f
 ORDER BY f.search_path IS NULL DESC, f.superuser DESC, f.public_execute DESC, f.signature
 LIMIT $1`

// functionSecurityCheck reports each database's SECURITY DEFINER functions:
// they run with their owner's privileges, so one that resolves names through
// the caller's search_path runs objects the caller planted.
type functionSecurityCheck struct{}

func (c *functionSecurityCheck) Name() string { return "function_security" }
func (c *functionSecurityCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *functionSecurityCheck) DefaultInterval() time.Duration { return 15 * time.Minute }
func (c *functionSecurityCheck) Timeout() time.Duration         { return 15 * time.Second }

func (c *functionSecurityCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, securityDefinerQuery, securityDefinerFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	var unpinned, exposed int
	for rows.Next() {
		var key, schema, name, args, kind, lang, owner string
		var superuser, public bool
		var searchPath *string
		if err := rows.Scan(&key, &schema, &name, &args, &kind, &lang, &owner, &superuser, &searchPath, &public); err != nil {
			return Result{}, err
		}
		if len(result.Facts) == securityDefinerFactLimit {
			result.Truncated = true
			break
		}
		state := "pinned"
		labels := map[string]string{
			"schemaname": schema, "name": name, "arguments": args, "kind": kind, "language": lang,
			"owner": owner, "owner_superuser": strconv.FormatBool(superuser), "public_execute": strconv.FormatBool(public),
			"search_path": "", "pg_temp_last": "false",
		}
		if searchPath == nil {
			state = "unpinned"
			unpinned++
			if superuser && public {
				exposed++
			}
		} else {
			labels["search_path"] = *searchPath
			labels["pg_temp_last"] = strconv.FormatBool(pgTempLast(*searchPath))
		}
		result.Facts = append(result.Facts, Fact{Kind: securityDefinerFactKind, Key: key, ValueText: state, Labels: labels})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_security_definer_functions", Value: float64(len(result.Facts)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_security_definer_functions_unpinned", Value: float64(unpinned), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_security_definer_functions_exposed", Value: float64(exposed), Kind: pgtype.KindGauge},
	)
	return result, nil
}

// pgTempLast reports whether a pinned search_path names pg_temp last. A
// path that does not name it searches the temporary schema first for tables
// and views, so a caller's temporary table can stand in for one the
// function means.
func pgTempLast(path string) bool {
	parts := strings.Split(path, ",")
	last := strings.Trim(strings.TrimSpace(parts[len(parts)-1]), `"`)
	return last == "pg_temp"
}
