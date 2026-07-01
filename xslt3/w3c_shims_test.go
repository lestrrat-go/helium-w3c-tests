package xslt3_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// testdata/xslt30 is gitignored; on a fresh checkout without a fetch the
	// fixtures are absent. Skip with a helpful message instead of failing every
	// case on a missing stylesheet, mirroring the xsd11 suite.
	if _, err := os.Stat(filepath.Join(w3cTestdataDir, "catalog.xml")); os.IsNotExist(err) {
		t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch xslt30")
	}
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
		p, ferr := w3cFileURIToPath(parsed, uri)
		if ferr != nil {
			return nil, ferr
		}
		target = p
	// A bare Windows absolute path ("C:\dir\x") is mis-parsed by url.Parse as
	// scheme "c"; treat single-letter drive schemes (and the empty scheme) as
	// local paths rather than rejecting them.
	case parsed.Scheme == "" || w3cIsWindowsDriveScheme(parsed):
		if filepath.IsAbs(uri) || w3cIsWindowsAbs(uri) {
			target = uri
		} else {
			target = filepath.Join(r.baseDir, uri)
		}
	default:
		return nil, fmt.Errorf("unsupported URI scheme: %s", parsed.Scheme)
	}

	return os.Open(target) //nolint:gosec // harness reads test fixtures by path
}

// w3cFileURIToPath converts a parsed "file:" URI to a native path, mirroring the
// upstream helium resolver's cross-platform semantics: reject non-local hosts,
// opaque/empty paths, and UNC forms, and on Windows strip the leading slash
// before a drive letter ("file:///C:/a" -> "C:\a").
func w3cFileURIToPath(parsed *url.URL, uri string) (string, error) {
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("non-local file URI host %q in %q", parsed.Host, uri)
	}
	if parsed.Opaque != "" || parsed.Path == "" {
		return "", fmt.Errorf("invalid file URI %q: no local path", uri)
	}
	p := parsed.Path
	// Reject UNC forms ("file:////server/share" -> "//server/share"), which
	// FromSlash would turn into a remote \\server\share path on Windows.
	if len(p) >= 2 && w3cIsPathSep(p[0]) && w3cIsPathSep(p[1]) {
		return "", fmt.Errorf("UNC file URI %q is not a local path", uri)
	}
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && w3cIsASCIILetter(p[1]) && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

func w3cIsWindowsDriveScheme(parsed *url.URL) bool {
	return len(parsed.Scheme) == 1 && w3cIsASCIILetter(parsed.Scheme[0])
}

// w3cIsWindowsAbs reports whether p is a Windows drive-absolute path ("C:\..."
// or "C:/...") regardless of host OS, so such a reference is kept absolute
// rather than joined against baseDir.
func w3cIsWindowsAbs(p string) bool {
	return len(p) >= 3 && w3cIsASCIILetter(p[0]) && p[1] == ':' && w3cIsPathSep(p[2])
}

func w3cIsASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func w3cIsPathSep(b byte) bool { return b == '/' || b == '\\' }

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
