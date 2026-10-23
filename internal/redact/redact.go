// Package redact decides whether a text read from a monitored database may
// hold a credential. The agent withholds such a text before it ships it, and
// the server applies the same rule to what older agents still send.
package redact

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Credentials reach a statement's text as SQL literals: the PASSWORD of a
// role, the password or sslpassword option of a server or user mapping, a
// value assigned or passed to a password column or parameter, the libpq
// string or URI of a subscription's CONNECTION or a dblink call, or the
// oauth_client_secret of a connection string. pg_stat_statements keeps
// utility statements as they were typed, and pg_stat_activity keeps every
// statement as it was sent, so these are read back as written.
//
// A redactor that removes only the value has to find where the value ends,
// which means reading the text as PostgreSQL's lexer does: quote parity under
// standard_conforming_strings, escape and Unicode strings, constants joined
// across lines, values built with || or format() inside bodies that EXECUTE
// runs later. Every gap in that reading left part of a secret in the text. So
// the text is not parsed: a statement that shows any sign of a credential is
// withheld whole, and only a statement with none is kept.

// Withheld stands for a text that is not shipped or stored because it may hold
// a credential.
const Withheld = "[redacted]"

// credentialWords are looked for in a text's letters and digits alone, so a
// word split by quotes, ||, blanks, comments or line breaks ('pass' || 'word')
// is found too. password covers sslpassword and the PASSWORD keyword, secret
// covers oauth_client_secret, and uescape marks a Unicode string whose escape
// character is not a backslash, which the decodings below do not read.
var credentialWords = []string{"password", "passwd", "secret", "uescape"}

// connectionURI is the scheme of a libpq URI, whose user info can carry a
// password. It is looked for in a text's letters, digits, colons and slashes,
// so a URI built from pieces ('postgresql:' || '//u:...') is found, while a
// cast such as 'postgres'::text is not.
var connectionURI = regexp.MustCompile(`postgres(?:ql)?:/`)

// MayHoldCredential reports whether a text shows a sign of a credential. The
// text is read as written, with the escapes of an escape string (E prefix)
// decoded, which every string has when standard_conforming_strings is off,
// and with the escapes of a Unicode string (U& prefix) decoded, so a keyword
// or scheme spelled with escapes is found in one of the readings. A sign
// outside any constant, such as a comment or a column named password, counts
// as well: a false sign costs the statement its text, a missed one ships a
// secret.
func MayHoldCredential(query string) bool {
	for _, text := range []string{query, decodeEscapeString(query), decodeUnicodeString(query)} {
		words := keepBytes(text, false)
		for _, word := range credentialWords {
			if strings.Contains(words, word) {
				return true
			}
		}
		if connectionURI.MatchString(keepBytes(text, true)) {
			return true
		}
	}
	return false
}

// keepBytes lowers a text's ASCII letters and keeps them with its digits,
// plus its colons and slashes when uri is set, dropping every other byte.
func keepBytes(text string, uri bool) string {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9':
			b.WriteByte(c)
		case 'A' <= c && c <= 'Z':
			b.WriteByte(c + 'a' - 'A')
		case uri && (c == ':' || c == '/'):
			b.WriteByte(c)
		}
	}
	return b.String()
}

// decodeEscapeString reads the whole text with the backslash escapes of an
// escape string (E prefix): \b \f \n \r \t, one to three octal digits, \x and
// one or two hex digits, \u and four or \U and eight hex digits. Any other
// escaped character stands for itself. The text outside strings is decoded
// too, which can only add signs.
func decodeEscapeString(text string) string {
	if strings.IndexByte(text, '\\') < 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '\\' || i+1 == len(text) {
			b.WriteByte(c)
			continue
		}
		i++
		switch c = text[i]; {
		case strings.IndexByte("bfnrt", c) >= 0:
			b.WriteByte(' ')
		case '0' <= c && c <= '7':
			v, n := readDigits(text[i:], 3, 8)
			b.WriteByte(byte(v))
			i += n - 1
		case c == 'x' && i+1 < len(text) && hexValue(text[i+1]) >= 0:
			v, n := readDigits(text[i+1:], 2, 16)
			b.WriteByte(byte(v))
			i += n
		case c == 'u' || c == 'U':
			width := 4
			if c == 'U' {
				width = 8
			}
			v, n := readDigits(text[i+1:], width, 16)
			if n != width {
				b.WriteByte(c)
				continue
			}
			b.WriteRune(rune(v))
			i += n
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// decodeUnicodeString reads the whole text with the escapes of a Unicode
// string (U& prefix) whose escape character is the default backslash: \ and
// four hex digits, or \+ and six. Any other escaped character stands for
// itself.
func decodeUnicodeString(text string) string {
	if strings.IndexByte(text, '\\') < 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '\\' || i+1 == len(text) {
			b.WriteByte(c)
			continue
		}
		width, from := 4, i+1
		if text[i+1] == '+' {
			width, from = 6, i+2
		}
		if v, n := readDigits(text[from:], width, 16); n == width && utf8.ValidRune(rune(v)) {
			b.WriteRune(rune(v))
			i = from + n - 1
			continue
		}
		i++
		b.WriteByte(text[i])
	}
	return b.String()
}

// readDigits reads up to limit digits of the base (8 or 16) from the start of
// s, and returns their value and how many it read.
func readDigits(s string, limit, base int) (value, n int) {
	for n < limit && n < len(s) {
		d := hexValue(s[n])
		if d < 0 || d >= base {
			break
		}
		value = value*base + d
		n++
	}
	return value, n
}

// hexValue is the value of a hex digit, or -1 for any other byte.
func hexValue(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	case 'A' <= c && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
