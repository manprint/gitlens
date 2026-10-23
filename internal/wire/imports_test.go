package wire_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestWire_ProductionFilesImportNoTesting keeps test code out of the binaries.
// The package is linked into the agent and the server, so a non-test file that
// imports testing or testify would ship the test framework in both, along with
// any flag it registers.
func TestWire_ProductionFilesImportNoTesting(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("import path in %s: %v", path, err)
			}
			if p == "testing" || strings.HasPrefix(p, "github.com/stretchr/testify") {
				t.Errorf("%s imports %s: test-only code in a production package", path, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no production files found")
	}
}
