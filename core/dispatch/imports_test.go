package dispatch_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoreDoesNotImportTransport enforces AC2 / AD-12 / AD-6: no package under
// core/ may import an edge adapter — a transport (core sees only the transport-
// agnostic message contract) or a renderer (core sees only the region-compositor
// contract). This makes a future accidental core→edge import fail the build, not
// merely a convention. The test runs with its working directory set to
// core/dispatch/, so ".." is the core/ tree.
func TestCoreDoesNotImportTransport(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "/transport") || strings.Contains(p, "telego") {
				t.Errorf("%s imports %q — core must not import a transport adapter (AD-12)", path, p)
			}
			if strings.Contains(p, "/display") {
				t.Errorf("%s imports %q — core must not import a renderer; the region-compositor contract is the only seam (AD-6)", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk core tree: %v", err)
	}
}
