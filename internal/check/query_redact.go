package check

import "regexp"

// Credentials reach a statement's text as SQL literals: the PASSWORD of a
// role, the password option of a server or user mapping, or the libpq string
// of a subscription's CONNECTION. pg_stat_statements keeps utility statements
// as they were typed, so these are read back exactly as the DBA wrote them.
var (
	// The literal may be unterminated: the text is cut at 8192 bytes, and a
	// password that straddles the cut must not survive. A dollar-quoted
	// literal runs to the end of the text for the same reason.
	sqlPasswordLiteral = regexp.MustCompile(`(?is)(\bpassword\s+)(?:[eE]'(?:[^'\\]|\\.|'')*'?|'(?:[^']|'')*'?|\$[A-Za-z_0-9]*\$.*)`)
	// A libpq value quoted inside a SQL literal doubles its quotes, so
	// ''a b'' is one value even though it holds a space.
	libpqPassword = regexp.MustCompile(`(?i)(\b(?:ssl)?password\s*=\s*)(?:''(?:[^']|'')*''|'(?:[^']|'')*'?|[^\s']+)`)
)

// redactQueryText hides the credentials a statement carries before its text
// is cached or shipped. The rest of the text is kept, so the statement stays
// readable and its shape still identifies it.
func redactQueryText(query string) string {
	query = sqlPasswordLiteral.ReplaceAllString(query, "${1}'[redacted]'")
	query = libpqPassword.ReplaceAllString(query, "${1}[redacted]")
	return conninfoURIPassword.ReplaceAllString(query, "${1}:[redacted]@")
}
