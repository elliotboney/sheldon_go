package broker_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// providerSDKs are the provider/LLM SDK module paths that may be imported ONLY
// under broker/internal/ (AD-9). Mirrors the .golangci.yml depguard rule as a
// go-test-level enforcement, so the invariant holds in the suite as well as in
// lint. Vacuous until Story 3.2 adds the first SDK; it then fails the moment one
// is imported outside broker/internal/.
var providerSDKs = []string{
	"github.com/anthropics/anthropic-sdk-go",
	"github.com/sashabaranov/go-openai",
	"github.com/ollama/ollama",
}

// TestProviderSDKsOnlyUnderBrokerInternal walks the whole repo (from the broker
// package dir, ".." is the repo root) and fails on any non-test .go file outside
// broker/internal/ that imports a provider SDK. Mirrors core/dispatch and
// core/scheduler imports tests; includes a scanned-file-count guard so a
// mis-rooted (empty) walk cannot pass vacuously (Story 2.5 review fix).
func TestProviderSDKsOnlyUnderBrokerInternal(t *testing.T) {
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/broker/internal/") {
			return nil // the one place provider SDKs are allowed
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // skip an unparseable file rather than disabling the whole fence
		}
		scanned++
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, sdk := range providerSDKs {
				if strings.HasPrefix(p, sdk) {
					t.Errorf("%s imports %q — provider SDKs may live only under broker/internal/ (AD-9)", path, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo tree: %v", err)
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d source files — fence check is vacuous (expected ≥10)", scanned)
	}
}
