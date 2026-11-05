package indexadvice

import (
	"errors"
	"strings"
)

type tokKind int

const (
	tIdent tokKind = iota
	tString
	tNumber
	tParam
	tOp
	tLParen
	tRParen
	tComma
	tDot
	tSemi
	tOther
)

// token is one lexical unit. Text is the folded value: unquoted identifiers
// are lower-cased, quoted ones are kept; strings hold their decoded body.
type token struct {
	kind   tokKind
	text   string
	quoted bool
	pos    int
	end    int
}

var errLex = errors.New("indexadvice: cannot tokenise")

const opChars = "+-*/<>=~!@#%^&|`?"

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// lex splits sql into tokens, dropping whitespace and comments. It never
// panics and always terminates: every iteration consumes at least one byte.
func lex(sql string) ([]token, error) {
	var toks []token
	n := len(sql)
	i := 0
	for i < n {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			depth := 1
			i += 2
			for i < n && depth > 0 {
				switch {
				case sql[i] == '/' && i+1 < n && sql[i+1] == '*':
					depth++
					i += 2
				case sql[i] == '*' && i+1 < n && sql[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			if depth > 0 {
				return nil, errLex
			}
		case c == '\'':
			body, next, ok := scanQuoted(sql, i, '\'', false)
			if !ok {
				return nil, errLex
			}
			toks = append(toks, token{kind: tString, text: body, pos: i, end: next})
			i = next
		case c == '"':
			body, next, ok := scanQuoted(sql, i, '"', false)
			if !ok {
				return nil, errLex
			}
			toks = append(toks, token{kind: tIdent, text: body, quoted: true, pos: i, end: next})
			i = next
		case c == '$':
			j := i + 1
			if j < n && isDigit(sql[j]) {
				for j < n && isDigit(sql[j]) {
					j++
				}
				toks = append(toks, token{kind: tParam, text: sql[i:j], pos: i, end: j})
				i = j
				break
			}
			for j < n && sql[j] != '$' && isIdentPart(sql[j]) && sql[j] != '$' {
				j++
			}
			if j >= n || sql[j] != '$' {
				return nil, errLex
			}
			tag := sql[i : j+1]
			close := strings.Index(sql[j+1:], tag)
			if close < 0 {
				return nil, errLex
			}
			bodyEnd := j + 1 + close
			toks = append(toks, token{kind: tString, text: sql[j+1 : bodyEnd], pos: i, end: bodyEnd + len(tag)})
			i = bodyEnd + len(tag)
		case isDigit(c) || (c == '.' && i+1 < n && isDigit(sql[i+1])):
			j := i
			for j < n && (isDigit(sql[j]) || sql[j] == '.' || sql[j] == '_') {
				j++
			}
			if j < n && (sql[j] == 'e' || sql[j] == 'E') {
				k := j + 1
				if k < n && (sql[k] == '+' || sql[k] == '-') {
					k++
				}
				if k < n && isDigit(sql[k]) {
					for k < n && isDigit(sql[k]) {
						k++
					}
					j = k
				}
			}
			toks = append(toks, token{kind: tNumber, text: sql[i:j], pos: i, end: j})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(sql[j]) {
				j++
			}
			word := sql[i:j]
			if j < n && sql[j] == '\'' && (word == "e" || word == "E") {
				body, next, ok := scanQuoted(sql, j, '\'', true)
				if !ok {
					return nil, errLex
				}
				toks = append(toks, token{kind: tString, text: body, pos: i, end: next})
				i = next
				break
			}
			if j < n && sql[j] == '\'' && len(word) == 1 && strings.ContainsRune("bBxXnN", rune(word[0])) {
				body, next, ok := scanQuoted(sql, j, '\'', false)
				if !ok {
					return nil, errLex
				}
				toks = append(toks, token{kind: tString, text: body, pos: i, end: next})
				i = next
				break
			}
			toks = append(toks, token{kind: tIdent, text: strings.ToLower(word), pos: i, end: j})
			i = j
		case c == '(':
			toks = append(toks, token{kind: tLParen, text: "(", pos: i, end: i + 1})
			i++
		case c == ')':
			toks = append(toks, token{kind: tRParen, text: ")", pos: i, end: i + 1})
			i++
		case c == ',':
			toks = append(toks, token{kind: tComma, text: ",", pos: i, end: i + 1})
			i++
		case c == ';':
			toks = append(toks, token{kind: tSemi, text: ";", pos: i, end: i + 1})
			i++
		case c == '.':
			toks = append(toks, token{kind: tDot, text: ".", pos: i, end: i + 1})
			i++
		case c == ':' && i+1 < n && sql[i+1] == ':':
			toks = append(toks, token{kind: tOp, text: "::", pos: i, end: i + 2})
			i += 2
		case strings.IndexByte(opChars, c) >= 0:
			j := i
			for j < n && strings.IndexByte(opChars, sql[j]) >= 0 {
				if sql[j] == '-' && j+1 < n && sql[j+1] == '-' {
					break
				}
				if sql[j] == '/' && j+1 < n && sql[j+1] == '*' {
					break
				}
				j++
			}
			if j == i {
				j = i + 1
			}
			toks = append(toks, token{kind: tOp, text: sql[i:j], pos: i, end: j})
			i = j
		default:
			toks = append(toks, token{kind: tOther, text: string(c), pos: i, end: i + 1})
			i++
		}
	}
	return toks, nil
}

// scanQuoted reads a quoted run starting at sql[start] (the opening quote). A
// doubled quote is one literal quote; with backslash a backslash escapes the
// next byte (E” strings). It returns the decoded body and the index after the
// closing quote.
func scanQuoted(sql string, start int, quote byte, backslash bool) (string, int, bool) {
	var b strings.Builder
	i := start + 1
	for i < len(sql) {
		c := sql[i]
		switch {
		case backslash && c == '\\' && i+1 < len(sql):
			b.WriteByte(sql[i+1])
			i += 2
		case c == quote && i+1 < len(sql) && sql[i+1] == quote:
			b.WriteByte(quote)
			i += 2
		case c == quote:
			return b.String(), i + 1, true
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", len(sql), false
}
