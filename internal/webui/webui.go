package webui

import (
	"embed"
	"errors"
	"io/fs"
)

// The all: prefix keeps dot-files and underscored files in generated asset trees.
//
//go:embed all:dist
var embedded embed.FS

// Assets returns the built single-page application rooted at dist/.
func Assets() (fs.FS, error) { return fs.Sub(embedded, "dist") }

// IsPlaceholder reports whether the embedded tree is the committed placeholder
// rather than a real build, which is true exactly when dist/.built is absent.
func IsPlaceholder() bool {
	_, err := fs.Stat(embedded, "dist/.built")
	return errors.Is(err, fs.ErrNotExist)
}
