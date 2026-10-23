package check

import (
	"regexp"
	"strings"
)

// Credentials reach a statement's text as SQL literals: the PASSWORD of a
// role, the password option of a server or user mapping, or the libpq string
// of a subscription's CONNECTION. pg_stat_statements keeps utility statements
// as they were typed, so these are read back exactly as the DBA wrote them.
var (
	// PASSWORD is found by its keyword. The blanks and comments that PostgreSQL
	// reads between it and its literal are skipped by skipPasswordSeparators,
	// because a regular expression cannot count the nesting of /* */ comments.
	passwordKeyword = regexp.MustCompile(`(?i)\bpassword`)
	// The literal may follow with no blank at all, or with a U& or E prefix. It
	// may be unterminated: the text is cut at 8192 bytes, and a password that
	// straddles the cut must not survive. A dollar-quoted literal runs to the end
	// of the text for the same reason. A backslash is an escape only in an
	// E-string. A plain literal ends at its first quote that is not doubled, which
	// is how PostgreSQL reads it with standard_conforming_strings on, the default.
	// That setting is not in the statement text, so with it off a plain literal
	// such as 'pa\'ss' ends after the backslash and the tail of the password stays
	// visible. An escape here would instead run a literal that ends in a backslash
	// on to the next quote under the default, and hide a later PASSWORD.
	passwordLiteral = regexp.MustCompile(`(?is)^(?:[eE]'(?:[^'\\]|\\.?|'')*'?|(?:U&)?'(?:[^']|'')*'?|\$[A-Za-z_0-9]*\$.*)`)
	// A libpq value quoted inside a SQL literal doubles its quotes, so
	// ''a b'' is one value even though it holds a space.
	libpqPassword = regexp.MustCompile(`(?i)(\b(?:ssl)?password\s*=\s*)(?:''(?:[^']|'')*''|'(?:[^']|'')*'?|[^\s']+)`)
)

// redactQueryText hides the credentials a statement carries before its text
// is cached or shipped. The rest of the text is kept, so the statement stays
// readable and its shape still identifies it.
func redactQueryText(query string) string {
	query = redactPasswordLiterals(query)
	query = libpqPassword.ReplaceAllString(query, "${1}[redacted]")
	return conninfoURIPassword.ReplaceAllString(query, "${1}:[redacted]@")
}

// redactPasswordLiterals replaces the literal that follows each PASSWORD
// keyword. A keyword that lies inside text already handled, a literal that was
// replaced or a comment that was skipped, is passed over, so the words of a
// password are not read as a keyword of their own.
func redactPasswordLiterals(query string) string {
	var out strings.Builder
	copied := 0
	for _, loc := range passwordKeyword.FindAllStringIndex(query, -1) {
		if loc[0] < copied {
			continue
		}
		start := skipPasswordSeparators(query, loc[1])
		lit := passwordLiteral.FindStringIndex(query[start:])
		if lit == nil {
			continue
		}
		out.WriteString(query[copied:start])
		out.WriteString("'[redacted]'")
		copied = start + lit[1]
	}
	out.WriteString(query[copied:])
	return out.String()
}

// skipPasswordSeparators returns the index just past the blanks and comments
// that PostgreSQL reads from s[i:] before a literal. A /* comment nests: an
// inner /* must be closed before the outer comment can end, so a quote inside
// the outer comment is never taken for the literal. A -- comment ends at a
// carriage return as well as at a line feed.
func skipPasswordSeparators(s string, i int) int {
	for i < len(s) {
		switch {
		case strings.IndexByte(" \t\n\r\f\v", s[i]) >= 0:
			i++
		case strings.HasPrefix(s[i:], "--"):
			end := strings.IndexAny(s[i:], "\r\n")
			if end < 0 {
				return len(s)
			}
			i += end
		case strings.HasPrefix(s[i:], "/*"):
			end := blockCommentEnd(s, i)
			if end < 0 {
				return len(s)
			}
			i = end
		default:
			return i
		}
	}
	return i
}

// blockCommentEnd returns the index just past the block comment that opens at
// s[i], or -1 when the text ends before the comment closes.
func blockCommentEnd(s string, i int) int {
	depth := 0
	for i+1 < len(s) {
		switch s[i : i+2] {
		case "/*":
			depth++
			i += 2
		case "*/":
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return -1
}
