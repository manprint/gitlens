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
