package webui

import (
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

func TestIsPlaceholder_TrueWithoutMarker(t *testing.T) {
	if !IsPlaceholder() {
		t.Fatal("expected the committed asset tree to be reported as a placeholder")
	}
}

func TestPlaceholderMentionsBuildCommand(t *testing.T) {
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
