package check

import (
	"regexp"
	"sort"
	"strings"
)

// Credentials reach a statement's text as SQL literals: the PASSWORD of a
// role, the password option of a server or user mapping, or the libpq string
// of a subscription's CONNECTION. pg_stat_statements keeps utility statements
// as they were typed, so these are read back exactly as the DBA wrote them.
//
// The statement is read the way PostgreSQL reads its top level: blanks,
// comments, identifiers, string constants and dollar-quoted bodies in turn.
// A keyword takes the string constant that follows it. A keyword inside a
// string constant or a dollar-quoted body is not read as a keyword, because
// that text may be run later by EXECUTE, so the text from that constant to the
// end of the statement is redacted. The value may then be built from several
// constants, which a local rule could not follow.
var (
	// The keyword is PASSWORD in any case, a quoted "password", or a U& name,
	// since the option list of a server or user mapping may name the option in
	// any of these spellings. Inside a constant or a body it is matched as text.
	// A bare PASSWORD must be a whole word, so a name such as password_hash does
	// not cut the text. Go's \b reads a $ or a byte above ASCII as a boundary,
	// where PostgreSQL reads a name character, so it can only over-redact.
	passwordKeyword = regexp.MustCompile(`(?i)\bpassword\b|"password"|U&"(?:[^"]|"")*"(?:\s*uescape\s*'(?:[^']|'')*')?`)
	// The value of a libpq key. libpq reads the string after the SQL literal
	// around it is unescaped, so a doubled quote inside a literal stands for one
	// quote, and a value in a dollar body holds its quotes as they are. An
	// unquoted value runs to a blank, so it may hold quotes, and a backslash
	// escapes the byte after it. A single-quoted value ends at an unescaped
	// quote. A value continued by a constant on the next line is one value, as
	// PostgreSQL joins the constants.
	libpqPassword = regexp.MustCompile(`(?is)(\b(?:ssl)?password\s*=\s*)(?:''(?:[^']|'')*''|'(?:\\.|[^'\\])*'?|(?:'\s*[\r\n]\s*'|\\.|[^\s\\])+)`)
)

// redactQueryText hides the credentials a statement carries before its text
// is cached or shipped. The rest of the text is kept, so the statement stays
// readable and its shape still identifies it.
func redactQueryText(query string) string {
	query = redactPasswordValues(query)
	query = redactLibpqValues(query)
	return conninfoURIPassword.ReplaceAllString(query, "${1}:[redacted]@")
}

// redactLibpqValues replaces the value of each libpq password key. A value
// that runs to the end of a constant or a body is cut before the quote or tag
// that closes it, since that delimiter is SQL, not part of the value.
func redactLibpqValues(query string) string {
	closers := closingDelimiters(query)
	var spans []textSpan
	for _, loc := range libpqPassword.FindAllStringSubmatchIndex(query, -1) {
		start, end := loc[3], loc[1]
		if n, ok := closers[end]; ok && end-n >= start {
			end -= n
		}
		if end > start {
			spans = append(spans, textSpan{start, end})
		}
	}
	return applySpans(query, spans, "[redacted]")
}

// closingDelimiters maps the end of each closed string constant and dollar
// body to the length of its closing delimiter.
func closingDelimiters(query string) map[int]int {
	closers := make(map[int]int)
	for i := 0; i < len(query); {
		t := scanToken(query, i)
		i = t.end
		if t.closing > 0 {
			closers[t.end] = t.closing
		}
	}
	return closers
}

// textSpan is a run of the statement text that is replaced by a redaction.
type textSpan struct{ start, end int }

// redactPasswordValues replaces the value of each PASSWORD keyword and the
// text from a constant or body that holds a keyword to the end of the
// statement. The text is read once, from the start, so the cost grows with
// its length.
func redactPasswordValues(query string) string {
	var spans []textSpan
	// cut is where the text is redacted to its end, or -1.
	cut := -1
	// backslash is the first plain constant with a backslash, or -1. With
	// standard_conforming_strings off that backslash escapes a quote, so the
	// constants after it may be read differently by PostgreSQL than here.
	backslash := -1
	// pending is set when the last token was a keyword, so the next constant
	// is its value.
	pending := false
	for i := 0; i < len(query); {
		t := scanToken(query, i)
		i = t.end
		switch t.kind {
		case tokBlank:
			// A blank or comment between a keyword and its value does not
			// end the keyword.
		case tokWord, tokName:
			pending = t.keyword
		case tokString, tokBody:
			if pending {
				spans = append(spans, textSpan{t.start, t.end})
				if t.backslash {
					cut = earlier(cut, t.start)
				}
			}
			pending = false
			if hasPasswordKeyword(query[t.start:t.end]) {
				cut = earlier(cut, t.start)
			}
			if t.backslash && backslash < 0 {
				backslash = t.start
			}
		default:
			pending = false
		}
	}
	if backslash >= 0 && hasPasswordKeyword(query[backslash:]) {
		cut = earlier(cut, backslash)
	}
	if cut >= 0 {
		spans = append(spans, textSpan{cut, len(query)})
	}
	return applySpans(query, spans, "'[redacted]'")
}

// earlier returns the smaller of cut and at, where a negative cut means none.
func earlier(cut, at int) int {
	if cut < 0 || at < cut {
		return at
	}
	return cut
}

// hasPasswordKeyword reports whether the text of a constant or a body holds a
// PASSWORD keyword that is not a libpq key. A key is followed by =, and no
// PASSWORD keyword of a statement is, so that case is left to the libpq rule.
func hasPasswordKeyword(text string) bool {
	for _, loc := range passwordKeyword.FindAllStringIndex(text, -1) {
		if !strings.HasPrefix(strings.TrimLeft(text[loc[1]:], " \t\r\n\f\v"), "=") {
			return true
		}
	}
	return false
}

// applySpans replaces each span of s with replacement. Spans that overlap or
// touch are replaced as one.
func applySpans(s string, spans []textSpan, replacement string) string {
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	var out strings.Builder
	copied := 0
	for i := 0; i < len(spans); {
		start, end := spans[i].start, spans[i].end
		for i++; i < len(spans) && spans[i].start <= end; i++ {
			end = max(end, spans[i].end)
		}
		out.WriteString(s[copied:start])
		out.WriteString(replacement)
		copied = end
	}
	out.WriteString(s[copied:])
	return out.String()
}

// tokenKind is the kind of a token of a statement.
type tokenKind int

const (
	tokOther  tokenKind = iota // a byte with no meaning here, or a parameter
	tokBlank                   // blanks and comments
	tokWord                    // an unquoted identifier or keyword
	tokName                    // a quoted identifier, or a U& identifier
	tokString                  // a string constant, with the constants that continue it
	tokBody                    // a dollar-quoted string
)

// token is one token of a statement, found the way PostgreSQL's scanner
// finds it.
type token struct {
	kind  tokenKind
	start int
	end   int
	// keyword is set for PASSWORD, a quoted "password", or a U& name.
	keyword bool
	// backslash is set for a plain string constant that holds a backslash.
	backslash bool
	// closing is the length of the delimiter that closes a string constant or
	// a dollar body, or 0 when the text ends before it.
	closing int
}

// stringMode is how a string constant reads a backslash.
type stringMode int

const (
	plainString   stringMode = iota // no escape, or an escape under standard_conforming_strings off
	escapeString                    // an E-string: a backslash escapes the next byte
	unicodeString                   // a U& string: no escape, only unicode escapes
)

// scanToken returns the token that starts at s[i].
func scanToken(s string, i int) token {
	switch c := s[i]; {
	case strings.IndexByte(" \t\n\r\f\v", c) >= 0:
		return token{kind: tokBlank, start: i, end: i + 1}
	case strings.HasPrefix(s[i:], "--"):
		return token{kind: tokBlank, start: i, end: lineCommentEnd(s, i)}
	case strings.HasPrefix(s[i:], "/*"):
		end := blockCommentEnd(s, i)
		if end < 0 {
			end = len(s)
		}
		return token{kind: tokBlank, start: i, end: end}
	case c == '\'':
		return stringToken(s, i, i, plainString)
	case c == '"':
		end := quotedEnd(s, i)
		return token{kind: tokName, start: i, end: end, keyword: strings.EqualFold(s[i:end], `"password"`)}
	case c == '$':
		if tagEnd := dollarTagEnd(s, i); tagEnd >= 0 {
			return dollarToken(s, i, tagEnd)
		}
		// A dollar with no tag after it is a parameter such as $1.
		end := i + 1
		for end < len(s) && isDigit(s[end]) {
			end++
		}
		return token{kind: tokOther, start: i, end: end}
	case isIdentStart(c):
		end := i + 1
		for end < len(s) && isIdentByte(s[end]) {
			end++
		}
		word := s[i:end]
		if end+1 < len(s) && s[end] == '&' && strings.EqualFold(word, "u") {
			switch s[end+1] {
			case '\'':
				return stringToken(s, i, end+1, unicodeString)
			case '"':
				return unicodeNameToken(s, i, end+1)
			}
		}
		if end < len(s) && s[end] == '\'' && strings.EqualFold(word, "e") {
			return stringToken(s, i, end, escapeString)
		}
		return token{kind: tokWord, start: i, end: end, keyword: strings.EqualFold(word, "password")}
	}
	return token{kind: tokOther, start: i, end: i + 1}
}

// stringToken returns the string constant whose opening quote is at s[q] and
// whose prefix, if any, starts at start.
func stringToken(s string, start, q int, mode stringMode) token {
	end, closed := stringChainEnd(s, q, mode == escapeString)
	t := token{
		kind:      tokString,
		start:     start,
		end:       end,
		backslash: mode == plainString && strings.IndexByte(s[start:end], '\\') >= 0,
	}
	if closed {
		t.closing = 1
	}
	return t
}

// unicodeNameToken returns the U& identifier whose opening quote is at s[q].
// PostgreSQL reads an UESCAPE clause as part of the name, so the clause is
// taken too, and the value after it is found.
func unicodeNameToken(s string, start, q int) token {
	end := quotedEnd(s, q)
	if k := skipSeparators(s, end); strings.EqualFold(s[k:min(k+7, len(s))], "uescape") {
		if m := skipSeparators(s, k+7); m < len(s) && s[m] == '\'' {
			end, _ = stringChainEnd(s, m, false)
		}
	}
	return token{kind: tokName, start: start, end: end, keyword: true}
}

// dollarToken returns the dollar-quoted string whose opening tag ends at
// tagEnd. It runs to the closing tag, or to the end of the text when the
// closing tag is not there.
func dollarToken(s string, start, tagEnd int) token {
	tag := s[start:tagEnd]
	end := len(s)
	closing := 0
	if i := strings.Index(s[tagEnd:], tag); i >= 0 {
		end = tagEnd + i + len(tag)
		closing = len(tag)
	}
	return token{kind: tokBody, start: start, end: end, closing: closing}
}

// dollarTagEnd returns the index just past the opening tag of a dollar-quoted
// string at s[i], or -1 when no tag follows. A tag is empty, or it holds
// letters, digits and underscores and does not start with a digit. PostgreSQL
// reads a byte of 0x80 and above as a letter, so a tag may hold one.
func dollarTagEnd(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		switch c := s[j]; {
		case c == '$':
			return j + 1
		case isIdentStart(c):
		case isDigit(c) && j > i+1:
		default:
			return -1
		}
	}
	return -1
}

// stringChainEnd returns the index just past the string constant that opens
// with the quote at s[q], and of each constant that continues it, and whether
// the last of them is closed. A constant continues in the same mode when a
// line break lies between it and the next one, with blanks and -- comments
// only. The text ends the constant when it ends first.
func stringChainEnd(s string, q int, escapes bool) (int, bool) {
	for {
		end := literalEnd(s, q, escapes)
		if end < 0 {
			return len(s), false
		}
		next, lineBreak := skipBlanks(s, end)
		if !lineBreak || next >= len(s) || s[next] != '\'' {
			return end, true
		}
		q = next
	}
}

// literalEnd returns the index just past the constant that opens with the
// quote at s[q], or -1 when the text ends first. A doubled quote stands for
// one quote. In an E-string a backslash escapes the byte after it.
func literalEnd(s string, q int, escapes bool) int {
	for i := q + 1; i < len(s); i++ {
		switch {
		case s[i] == '\\' && escapes:
			i++
		case s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case s[i] == '\'':
			return i + 1
		}
	}
	return -1
}

// quotedEnd returns the index just past the quoted identifier that opens with
// the quote at s[q], or len(s) when the text ends first. A doubled quote
// stands for one quote.
func quotedEnd(s string, q int) int {
	for i := q + 1; i < len(s); i++ {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(s)
}

// skipBlanks returns the index just past the blanks and -- comments that s[i:]
// starts with, and whether a carriage return or line feed was among them. A
// -- comment ends at a carriage return as well as at a line feed.
func skipBlanks(s string, i int) (int, bool) {
	lineBreak := false
	for i < len(s) {
		switch {
		case s[i] == '\n' || s[i] == '\r':
			lineBreak = true
			i++
		case strings.IndexByte(" \t\f\v", s[i]) >= 0:
			i++
		case strings.HasPrefix(s[i:], "--"):
			i = lineCommentEnd(s, i)
		default:
			return i, lineBreak
		}
	}
	return i, lineBreak
}

// skipSeparators returns the index just past the blanks and comments that
// s[i:] starts with, nested /* */ comments included.
func skipSeparators(s string, i int) int {
	for {
		i, _ = skipBlanks(s, i)
		if !strings.HasPrefix(s[i:], "/*") {
			return i
		}
		end := blockCommentEnd(s, i)
		if end < 0 {
			return len(s)
		}
		i = end
	}
}

// lineCommentEnd returns the index of the carriage return or line feed that
// ends the -- comment at s[i:], or the length of the text when none follows.
func lineCommentEnd(s string, i int) int {
	if end := strings.IndexAny(s[i:], "\r\n"); end >= 0 {
		return i + end
	}
	return len(s)
}

// blockCommentEnd returns the index just past the block comment that opens at
// s[i], or -1 when the text ends before the comment closes. A /* comment
// nests: an inner /* must be closed before the outer comment can end.
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

// isIdentStart reports whether c can start an unquoted identifier.
func isIdentStart(c byte) bool {
	return c == '_' || c >= 0x80 || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isIdentByte reports whether c can continue an unquoted identifier. PostgreSQL
// allows a dollar sign in an identifier, so a $ after a name is part of it.
func isIdentByte(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '$'
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
