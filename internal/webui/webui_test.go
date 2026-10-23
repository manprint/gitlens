package webui

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestAssets_ContainsIndexHTML(t *testing.T) {
	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets() error: %v", err)
	}

	contents, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	if len(strings.TrimSpace(string(contents))) == 0 {
		t.Fatal("embedded index.html is empty")
	}
}

// The interface links to these documents, so a build that leaves one out
// sends the operator to a 404.
func TestAssets_ContainsDocuments(t *testing.T) {
	if IsPlaceholder() {
		t.Skip("placeholder is embedded; run make web-build to embed the documents")
	}

	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets() error: %v", err)
	}
	for _, name := range []string{"README.md", "docs/LIMITS.md", "docs/api.md"} {
		if _, err := fs.Stat(assets, name); err != nil {
			t.Errorf("embedded %s: %v", name, err)
		}
	}
}

func TestIsPlaceholderMatchesMarker(t *testing.T) {
	_, err := fs.Stat(embedded, "dist/.built")
	wantPlaceholder := errors.Is(err, fs.ErrNotExist)
	if got := IsPlaceholder(); got != wantPlaceholder {
		t.Fatalf("IsPlaceholder() = %v, want %v for dist/.built error %v", got, wantPlaceholder, err)
	}
}

func TestPlaceholderMentionsBuildCommand(t *testing.T) {
	if !IsPlaceholder() {
		t.Skip("real frontend build is embedded")
	}

	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets() error: %v", err)
	}

	contents, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	if !strings.Contains(string(contents), "make web-build") {
		t.Fatal("placeholder does not mention make web-build")
	}
}
