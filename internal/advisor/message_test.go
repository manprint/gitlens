package advisor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/i18n"
)

// TestMain turns on strictMessages: a rule whose message misses a catalog
// entry or an argument panics in its test.
func TestMain(m *testing.M) {
	strictMessages = true
	os.Exit(m.Run())
}

func TestCheckMessage_RefusesAMissingArgument(t *testing.T) {
	require.PanicsWithValue(t, "advisor message function.untracked.title lacks argument database", func() {
		checkMessage("function.untracked", i18n.Args{})
	})
	require.PanicsWithValue(t, "advisor message no.such has no title", func() {
		checkMessage("no.such", i18n.Args{})
	})
}

// TestMessage_EveryKeyRendersInEveryLocale (I18N-004, plan 006 F5): each
// advisor message has a title and a detail in the reference catalog, and a
// stored message survives the JSON round trip into the same English text.
func TestMessage_EveryKeyRendersInEveryLocale(t *testing.T) {
	f := Finding{RuleID: "function.untracked"}.say("function.untracked", i18n.Args{"database": "app", "count": 3, "databaseIdent": `"app"`})
	require.Equal(t, "Function calls are not tracked in database app", f.Title)
	require.Contains(t, f.Detail, "its 3 functions in a procedural language")

	raw, err := json.Marshal(f.Message)
	require.NoError(t, err)
	var back Message
	require.NoError(t, json.Unmarshal(raw, &back))
	title, detail, remediation := back.Render(i18n.Default)
	require.Equal(t, f.Title, title)
	require.Equal(t, f.Detail, detail)
	require.Equal(t, f.Remediation, remediation)

	title, detail, _ = back.Render("it")
	require.Equal(t, "Le chiamate di funzione non sono tracciate nel database app", title)
	require.Contains(t, detail, "le sue 3 funzioni")
	for _, text := range []string{title, detail} {
		require.False(t, strings.Contains(text, "{"), text)
	}
}
