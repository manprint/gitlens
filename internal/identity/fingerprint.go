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
// Normalization, in order: parse the DSN; drop the password; lowercase host;
// apply the default port 5432 when absent; sort query parameters by key and
// drop those that do not affect target identity (application_name,
// connect_timeout, statement_timeout, sslmode); re-render; SHA-256; hex.
func Fingerprint(dsn string) (string, error) {
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
	if u.Host == "" {
		return "", fmt.Errorf("fingerprint: missing host")
	}

	// User: keep username, drop password
	var user string
	if u.User != nil {
		user = u.User.Username()
	}

	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("fingerprint: missing hostname")
	}
	hostname = strings.ToLower(hostname)
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	hostport := net.JoinHostPort(hostname, port)

	// Database (path without leading slash)
	dbname := strings.TrimPrefix(u.Path, "/")

	// Query params: sort keys, drop non-identity params
	ignore := map[string]bool{
		"application_name":  true,
		"connect_timeout":   true,
		"statement_timeout": true,
		"sslmode":           true,
	}
	values := u.Query()
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
