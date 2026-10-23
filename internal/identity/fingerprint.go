package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

// Fingerprint derives a stable, non-reversible key for a DSN. The password is
// removed before hashing and the result never contains any part of the DSN,
// so a fingerprint is safe to log; a DSN never is.
//
// Normalization, in order: parse the DSN; drop the password, whether it is in
// the userinfo or in the query, where pgx reads it too; move a host and port
// given in the query into the authority when the authority has none (Unix
// socket and host-in-query DSNs); lowercase host unless it is a socket
// directory; apply the default port 5432 when absent; sort query parameters by
// key and drop those that do not affect target identity (application_name,
// connect_timeout, statement_timeout, sslmode, password, sslpassword); re-render;
// SHA-256; hex.
//
// A DSN that names no host at all connects to the local default socket, so
// its identity is that of the local server: the empty host, as pgx reads it.
func Fingerprint(dsn string) (string, error) {
	return fingerprint(dsn, false)
}

// legacyFingerprint is the fingerprint the rule before Fingerprint gave the DSN,
// when a password written in its query still counted toward the identity. It
// is the empty string when that rule gives the same value as Fingerprint, which
// is every DSN without such a password.
func legacyFingerprint(dsn string) string {
	current, err := fingerprint(dsn, false)
	if err != nil {
		return ""
	}
	legacy, err := fingerprint(dsn, true)
	if err != nil || legacy == current {
		return ""
	}
	return legacy
}

// fingerprint implements Fingerprint. keepQuerySecrets selects the rule before
// the query password was dropped from the identity, for legacyFingerprint only.
func fingerprint(dsn string, keepQuerySecrets bool) (string, error) {
	if dsn == "" {
		return "", fmt.Errorf("empty dsn")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("fingerprint: parse dsn: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("fingerprint: unsupported scheme %q", u.Scheme)
	}

	// User: keep username, drop password
	var user string
	if u.User != nil {
		user = u.User.Username()
	}

	// Query params: sort keys, drop non-identity params
	values := u.Query()

	// pgx takes the host and port from the query when the authority has no
	// host, as TargetIsLocal reads them: postgres:///db?host=/var/run/postgresql
	// names a socket directory. They are moved into the authority here, so
	// the same server fingerprints the same however the DSN spells it.
	var hostname, port string
	if u.Host != "" {
		hostname = u.Hostname()
		port = u.Port()
	} else {
		hostname = values.Get("host")
		port = values.Get("port")
		values.Del("host")
		values.Del("port")
	}
	// A socket directory is a case-sensitive path, not a DNS name.
	if !strings.HasPrefix(hostname, "/") {
		hostname = strings.ToLower(hostname)
	}
	if port == "" {
		port = "5432"
	}
	hostport := net.JoinHostPort(hostname, port)

	// Database (path without leading slash)
	dbname := strings.TrimPrefix(u.Path, "/")

	// A password in the query is a secret: it must not reach the hash, or a
	// rotated one would register the server as a new instance.
	ignore := map[string]bool{
		"application_name":  true,
		"connect_timeout":   true,
		"statement_timeout": true,
		"sslmode":           true,
	}
	if !keepQuerySecrets {
		ignore["password"] = true
		ignore["sslpassword"] = true
	}
	var keys []string
	for k := range values {
		if ignore[strings.ToLower(k)] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var qparts []string
	for _, k := range keys {
		// values[k] is slice; join with proper encoding per key sorted values?
		vals := values[k]
		sort.Strings(vals)
		for _, v := range vals {
			qparts = append(qparts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
		}
	}
	query := strings.Join(qparts, "&")

	// Canonical form
	var canonical string
	if query != "" {
		canonical = fmt.Sprintf("postgres://%s@%s/%s?%s", user, hostport, dbname, query)
	} else {
		canonical = fmt.Sprintf("postgres://%s@%s/%s", user, hostport, dbname)
	}

	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:]), nil
}
