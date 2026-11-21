package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The console's language registry and message format cases (plan 006 D1, D3).
var webI18n = filepath.Join("..", "..", "web", "src", "i18n")

func readWebJSON(t *testing.T, name string, out any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(webI18n, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// I18N-004: the format cases the console runs give the same text here.
func TestSharedFormatCases(t *testing.T) {
	var cases []struct {
		Locale  string
		Message string
		Args    Args
		Want    string
	}
	readWebJSON(t, filepath.Join("testdata", "format-cases.json"), &cases)
	if len(cases) == 0 {
		t.Fatal("no format cases")
	}
	for _, c := range cases {
		if got := Format(c.Locale, c.Message, c.Args); got != c.Want {
			t.Errorf("%s %q: got %q, want %q", c.Locale, c.Message, got, c.Want)
		}
	}
}

// I18N-010: every server catalog is a console language, and every stable
// console language has a server catalog, so notifications follow the console.
func TestRegistryParity(t *testing.T) {
	var registry []struct {
		Code   string
		Status string
	}
	readWebJSON(t, "locales.json", &registry)
	listed := map[string]bool{}
	for _, l := range registry {
		listed[l.Code] = true
		if _, ok := catalogs[l.Code]; l.Status == "stable" && !ok {
			t.Errorf("stable console language %q has no locales/%s.json", l.Code, l.Code)
		}
	}
	for _, code := range Locales() {
		if !listed[code] {
			t.Errorf("locales/%s.json is not in web/src/i18n/locales.json", code)
		}
	}
}
