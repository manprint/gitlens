package i18n

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestCatalogParity(t *testing.T) {
	ref := catalogs[Default]
	for _, code := range Locales() {
		cat := catalogs[code]
		for k, msg := range ref {
			other, ok := cat[k]
			if !ok {
				t.Errorf("%s: missing key %q", code, k)
				continue
			}
			if a, b := Placeholders(msg), Placeholders(other); !reflect.DeepEqual(a, b) {
				t.Errorf("%s: %q placeholders %v, en has %v", code, k, b, a)
			}
		}
		for k := range cat {
			if _, ok := ref[k]; !ok {
				t.Errorf("%s: key %q not in %s", code, k, Default)
			}
		}
	}
}

func TestLocalesAndResolve(t *testing.T) {
	got := Locales()
	if !sort.StringsAreSorted(got) || len(got) < 2 {
		t.Fatalf("Locales() = %v", got)
	}
	for in, want := range map[string]string{"it": "it", "IT-it": "it", "it_IT": "it", " en ": "en", "xx": "en", "": "en", "xx-it": "en"} {
		if r := Resolve(in); r != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, r, want)
		}
	}
}

func TestT(t *testing.T) {
	if got := T("it", "field.problem", nil); got != "Problema" {
		t.Errorf("it field.problem = %q", got)
	}
	if got := T("fr", "field.problem", nil); got != "Problem" {
		t.Errorf("fr falls back to en, got %q", got)
	}
	if got := T("it", "no.such.key", nil); got != "no.such.key" {
		t.Errorf("unknown key = %q", got)
	}
	catalogs[Default]["test.only_en"] = "x {n}"
	defer delete(catalogs[Default], "test.only_en")
	if got := T("it", "test.only_en", Args{"n": 1.5}); got != "x 1.5" {
		t.Errorf("en fallback keeps en number format, got %q", got)
	}
	if !Has("field.problem") || Has("no.such.key") {
		t.Error("Has")
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		locale, msg string
		args        Args
		want        string
	}{
		{"en", "a {x} b", Args{"x": "y"}, "a y b"},
		{"en", "a {missing} b", nil, "a {missing} b"},
		{"en", "{n, plural, =0 {none} one {# row} other {# rows}}", Args{"n": 0}, "none"},
		{"en", "{n, plural, =0 {none} one {# row} other {# rows}}", Args{"n": 1}, "1 row"},
		{"en", "{n, plural, one {# row} other {# rows}}", Args{"n": int64(3)}, "3 rows"},
		{"en", "{n, plural, one {# row} other {# rows}}", Args{"n": 1.5}, "1.5 rows"},
		{"it", "{n, plural, one {# riga} other {# righe}}", Args{"n": 2.25}, "2,25 righe"},
		{"fr", "{n, plural, one {# ligne} other {# lignes}}", Args{"n": 1.5}, "1,5 ligne"},
		{"en", "{n, plural, one {x}}", Args{"n": 2}, ""},
		{"en", "{r, select, primary {P} other {O}}", Args{"r": "primary"}, "P"},
		{"en", "{r, select, primary {P} other {O}}", Args{"r": "standby"}, "O"},
		{"en", "{n, plural, other {# in {where}}}", Args{"n": 2, "where": "db"}, "2 in db"},
		{"en", "{n, plural, one {# {k, select, a {A#} other {B}}} other {x}}", Args{"n": 1, "k": "a"}, "1 A1"},
		{"en", "{x, bogus, y}", Args{"x": 1}, "{x, bogus, y}"},
		{"en", "open { brace", nil, "open { brace"},
		{"en", "{n, plural, one {a} other", Args{"n": 1}, "{n, plural, one {a} other"},
		{"en", "{n, plural, one}", Args{"n": 1}, ""},
		{"en", "{n}", Args{"n": float32(0.5)}, "0.5"},
		{"en", "{n}", Args{"n": int32(7)}, "7"},
		{"en", "#{n}", Args{"n": 7}, "#7"},
	}
	for _, c := range cases {
		if got := Format(c.locale, c.msg, c.args); got != c.want {
			t.Errorf("Format(%s, %q) = %q, want %q", c.locale, c.msg, got, c.want)
		}
	}
}

func TestNumberAndPlural(t *testing.T) {
	if Number("en", 1234.5) != "1234.5" || Number("it", 1234.5) != "1234,5" || Number("en", 2) != "2" || Number("de", 0.333) != "0,33" {
		t.Error("Number")
	}
	for _, c := range []struct {
		loc  string
		n    float64
		want string
	}{{"en", 1, "one"}, {"en", 1.5, "other"}, {"it", 0, "other"}, {"de", 1, "one"}, {"fr", 0, "one"}, {"fr", 1.9, "one"}, {"fr", 2, "other"}} {
		if got := PluralCategory(c.loc, c.n); got != c.want {
			t.Errorf("PluralCategory(%s, %v) = %s", c.loc, c.n, got)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("{a} {n, plural, one {# {b}} other {{c}}} {d, select, x {{e}} other {}} {unclosed")
	if want := []string{"a", "b", "c", "d", "e", "n"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Placeholders = %v", got)
	}
}

// A Fixed argument keeps its digits and takes the locale's decimal mark, also
// after the JSON round trip of a stored finding message.
func TestFixed(t *testing.T) {
	v := Fixed{V: 12.3, Digits: 2}
	if got := Value("en", v); got != "12.30" {
		t.Fatalf("en = %q", got)
	}
	if got := Value("it", v); got != "12,30" {
		t.Fatalf("it = %q", got)
	}
	raw, err := json.Marshal(Args{"x": v})
	if err != nil {
		t.Fatal(err)
	}
	var back Args
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got := Value("it", back["x"]); got != "12,30" {
		t.Fatalf("after JSON it = %q (%s)", got, raw)
	}
	if got := Value("en", map[string]any{"$fixed": "1.5", "other": 1}); got == "1.5" {
		t.Fatalf("a map with other keys is not a Fixed: %q", got)
	}
}
