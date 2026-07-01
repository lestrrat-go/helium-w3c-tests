package xslt3_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lestrrat-go/helium/xpath3"
)

// xslt30AllCases holds every generated W3C XSLT 3.0 conformance case. Each
// generated xslt30_<category>_gen_test.go registers its cases via init(), and
// TestXSLT30W3C runs them all as subtests under one root test.
var xslt30AllCases []w3cTest

// TestXSLT30W3C is the single entry point for the W3C XSLT 3.0 conformance
// suite. Running every case as a subtest of one root test keeps the JUnit
// reporter (cmd/w3ctest) simple and mirrors the xsd11 suite's TestXSD11W3C.
func TestXSLT30W3C(t *testing.T) {
	w3cRunTests(t, xslt30AllCases)
}

// seqLen returns the number of items in an xpath3 sequence, treating a nil
// sequence as empty.
func seqLen(s xpath3.Sequence) int {
	if s == nil {
		return 0
	}
	return s.Len()
}

// seqMaterialize returns the items of an xpath3 sequence as a plain slice,
// treating a nil sequence as nil.
func seqMaterialize(s xpath3.Sequence) []xpath3.Item {
	if s == nil {
		return nil
	}
	return s.Materialize()
}

// w3cFileURIResolver resolves file:// URIs and relative/absolute file paths for
// the conformance harness. It implements xpath3.URIResolver. The harness only
// ever uses baseDir="/" (the filesystem root), so no additional confinement is
// applied beyond opening the resolved path.
type w3cFileURIResolver struct {
	baseDir string
}

func (r *w3cFileURIResolver) ResolveURI(uri string) (io.ReadCloser, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	var target string
	switch {
	case parsed.Scheme == "file":
		// Only local file URIs are supported: a non-empty, non-localhost host
		// (e.g. "file://evil/path") would otherwise be silently dropped and the
		// path treated as local. Opaque forms have no usable path.
		if parsed.Host != "" && parsed.Host != "localhost" {
			return nil, fmt.Errorf("non-local file URI host %q", parsed.Host)
		}
		if parsed.Path == "" {
			return nil, fmt.Errorf("file URI has no path: %s", uri)
		}
		// POSIX: "file:///a/b" -> "/a/b". On Windows url.Path holds a leading
		// slash before the drive letter; FromSlash keeps the native separators.
		target = filepath.FromSlash(parsed.Path)
	case parsed.Scheme == "":
		if filepath.IsAbs(uri) {
			target = uri
		} else {
			target = filepath.Join(r.baseDir, uri)
		}
	default:
		return nil, fmt.Errorf("unsupported URI scheme: %s", parsed.Scheme)
	}

	return os.Open(target) //nolint:gosec // harness reads test fixtures by path
}

// w3cExpectations holds per-case skip/xfail overrides for the XSLT 3.0 suite,
// loaded from expectations/xslt30.json. Ad-hoc skips for tests that are valid
// per spec but exercise behavior the processor does not follow live here rather
// than being baked into the generated case tables, so they can be tuned without
// regenerating.
type w3cExpectations struct {
	Skip  map[string]string `json:"skip"`
	XFail map[string]string `json:"xfail"`
}

var (
	w3cExpectationsOnce sync.Once
	w3cExpectationsData w3cExpectations
)

// w3cExpectationSkips returns the loaded skip map. XSLT30_EXPECTATIONS overrides
// the default expectations/xslt30.json path (so parallel chunks can point at
// their own copy). A missing file yields an empty map; a present-but-unreadable
// or malformed file panics (a silent parse failure would disable every skip and
// surface as confusing test failures).
func w3cExpectationSkips() map[string]string {
	w3cExpectationsOnce.Do(func() {
		p := os.Getenv("XSLT30_EXPECTATIONS")
		if p == "" {
			p = filepath.Join("..", "expectations", "xslt30.json")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			panic(fmt.Sprintf("read expectations %s: %v", p, err))
		}
		if err := json.Unmarshal(data, &w3cExpectationsData); err != nil {
			panic(fmt.Sprintf("parse expectations %s: %v", p, err))
		}
	})
	return w3cExpectationsData.Skip
}
