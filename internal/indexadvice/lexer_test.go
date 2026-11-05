package indexadvice

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func texts(t *testing.T, sql string) []string {
	t.Helper()
	toks, err := lex(sql)
	require.NoError(t, err)
	out := make([]string, len(toks))
	for i, tok := range toks {
		out[i] = tok.text
	}
	return out
}

func TestIDXALEX001_QuotedIdentifiers(t *testing.T) {
	toks, err := lex(`select "Res Partner"."a""b" from t`)
	require.NoError(t, err)
	require.Equal(t, "Res Partner", toks[1].text)
	require.True(t, toks[1].quoted)
	require.Equal(t, `a"b`, toks[3].text)
	require.Equal(t, "select", toks[0].text)
}

func TestIDXALEX002_SingleQuoteEscape(t *testing.T) {
	require.Equal(t, []string{"where", "name", "=", "it's", "and", "x"}, texts(t, `WHERE name = 'it''s' AND x`))
	toks, err := lex(`select E'a\'b'`)
	require.NoError(t, err)
	require.Equal(t, "a'b", toks[1].text)
}

func TestIDXALEX003_DollarQuoting(t *testing.T) {
	require.Equal(t, []string{"select", "a ; b", "from", "t"}, texts(t, `select $$a ; b$$ from t`))
	require.Equal(t, []string{"select", "x $$ y", "from", "t"}, texts(t, `select $tag$x $$ y$tag$ from t`))
	_, err := lex(`select $tag$never closed`)
	require.Error(t, err)
}

func TestIDXALEX004_Comments(t *testing.T) {
	require.Equal(t, []string{"select", "a", "from", "t"}, texts(t, "select a -- trailing\nfrom /* block /* nested */ still */ t"))
	_, err := lex("select /* open")
	require.Error(t, err)
}

func TestIDXALEX005_Parameters(t *testing.T) {
	toks, err := lex(`a = $1 and b = $12`)
	require.NoError(t, err)
	require.Equal(t, tParam, toks[2].kind)
	require.Equal(t, "$12", toks[6].text)
}

func TestIDXALEX006_Operators(t *testing.T) {
	require.Equal(t, []string{"a", "<>", "b", "!=", "c", "<=", "d", ">=", "e", "~~", "f", "!~~", "g", "::", "text"},
		texts(t, `a <> b != c <= d >= e ~~ f !~~ g ::text`))
	require.Equal(t, []string{"a", "=", "b"}, texts(t, "a =--c\nb"))
}

func TestIDXALEX007_UnterminatedStringAndTotal(t *testing.T) {
	_, err := lex(`select 'open`)
	require.Error(t, err)
	_, err = lex(`select "open`)
	require.Error(t, err)
	for _, in := range []string{"", "   ", "\x00\xff", strings.Repeat("(", 1000), "$", "$$"} {
		_, _ = lex(in)
	}
}
