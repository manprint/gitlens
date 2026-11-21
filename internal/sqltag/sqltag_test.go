package sqltag

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const r11 = `SELECT 1 /*action='run',controller='orders',route='%2Forders%2F%3Aid',traceparent='00-5bd66ef5095369c7b0d1f8f4bd33716a-c532cb4098ac3dd2-01'*/`

func TestQTAG001(t *testing.T) {
	tags := Parse(r11)
	require.Equal(t, map[string]string{
		"action":      "run",
		"controller":  "orders",
		"route":       "/orders/:id",
		"traceparent": "00-5bd66ef5095369c7b0d1f8f4bd33716a-c532cb4098ac3dd2-01",
	}, tags)
	require.Equal(t, "/orders/:id", Endpoint(tags))
	require.Equal(t, "5bd66ef5095369c7b0d1f8f4bd33716a", TraceID(tags))
}

func TestQTAG002(t *testing.T) {
	tags := Parse("SELECT 1 /* application='it\\'s%20shop' ,  route='%2Fcart' */ ;  \n")
	require.Equal(t, map[string]string{"application": "it's shop", "route": "/cart"}, tags)
	require.Equal(t, "it's shop", Service(tags))

	// A pair that does not decode is skipped; the others stay.
	require.Equal(t, map[string]string{"route": "/x"}, Parse(`SELECT 1 /*application='%zz',route='%2Fx'*/`))
}

func TestQTAG003(t *testing.T) {
	many := make([]string, 21)
	for i := range many {
		many[i] = "k" + strings.Repeat("x", i) + "='v'"
	}
	twenty := strings.Join(many[:20], ",")
	for name, text := range map[string]string{
		"no comment":      "SELECT 1",
		"middle comment":  "SELECT /*application='shop'*/ 1",
		"hint":            "SELECT /*+ SeqScan(t) */ 1 FROM t /*+ IndexScan(t) */",
		"hint tag shaped": "SELECT 1 /*+application='shop'*/",
		"long body":       "SELECT 1 /*application='" + strings.Repeat("a", 4100) + "'*/",
		"21 pairs":        "SELECT 1 /*" + strings.Join(many, ",") + "*/",
		"plain comment":   "SELECT 1 /* refresh orders */",
		"unterminated":    "SELECT 1 /*application='shop*/",
		"empty":           "SELECT 1 /**/",
		"no key":          "SELECT 1 /*='shop'*/",
		"trailing comma":  "SELECT 1 /*application='shop',*/",
		"unquoted":        "SELECT 1 /*application=shop*/",
	} {
		require.Empty(t, Parse(text), name)
	}
	require.Len(t, Parse("SELECT 1 /*"+twenty+"*/"), 20)
}

func TestQTAG004(t *testing.T) {
	require.Equal(t, "/r", Endpoint(map[string]string{"route": "/r", "controller": "c", "action": "a"}))
	require.Equal(t, "c#a", Endpoint(map[string]string{"controller": "c", "action": "a"}))
	require.Equal(t, "c", Endpoint(map[string]string{"controller": "c"}))
	require.Equal(t, "a", Endpoint(map[string]string{"action": "a"}))
	require.Empty(t, Endpoint(map[string]string{}))

	for _, bad := range []string{
		"00-5BD66EF5095369C7B0D1F8F4BD33716A-c532cb4098ac3dd2-01",
		"00-5bd66ef5095369c7b0d1f8f4bd33716-c532cb4098ac3dd2-01",
		"5bd66ef5095369c7b0d1f8f4bd33716a",
		"00-" + strings.Repeat("0", 32) + "-c532cb4098ac3dd2-01",
		"",
	} {
		require.Empty(t, TraceID(map[string]string{"traceparent": bad}), bad)
	}
}

func TestQTAG005(t *testing.T) {
	require.Empty(t, Service(map[string]string{"application": "shop password=hunter2"}))
	require.Empty(t, Endpoint(map[string]string{"route": "postgresql://u:p@h/db"}))
	require.Empty(t, Service(map[string]string{"application": "bad\x01name"}))
	require.Empty(t, Service(map[string]string{"application": "bad\xffname"}))
	long := strings.Repeat("a", 300)
	require.Equal(t, long[:128], Service(map[string]string{"application": long}))
	// A cut never splits a character.
	multi := strings.Repeat("a", 127) + "é" + "tail"
	require.Equal(t, strings.Repeat("a", 127), Service(map[string]string{"application": multi}))
}

func FuzzParse(f *testing.F) {
	f.Add(r11)
	f.Add("SELECT 1 /*a='b',c='d'*/;")
	f.Add("SELECT 1 /*+ hint */")
	f.Add("/*a='\\''*/")
	f.Fuzz(func(t *testing.T, text string) {
		tags := Parse(text)
		if len(tags) > maxPairs {
			t.Fatalf("%d pairs", len(tags))
		}
		_ = Service(tags)
		_ = Endpoint(tags)
		_ = TraceID(tags)
	})
}
