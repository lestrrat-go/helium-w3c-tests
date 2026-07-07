package xpath3_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// QT3 fixture doc paths used by the hand-written collection / source-document
// tests (qt3_collection_test.go, qt3_source_docs_test.go).
const (
	docPathWorks = "docs/works.xml"
	docPathStaff = "docs/staff.xml"
	docNameWorks = "works"
	docNameStaff = "staff"
	expr1To10    = "1 to 10"

	// qtFotsCatalogNS is the QT3 FOTS catalog namespace (note the trailing slash,
	// distinct from the generator's catalog-parsing namespace).
	qtFotsCatalogNS = "http://www.w3.org/2010/09/qt-fots-catalog/"
)

// qt3AllCases holds every generated QT3 conformance case. Each generated
// xpath3/qt3_<category>_gen_test.go registers its cases via init(), and
// TestQT3W3C runs them all as subtests under one root test, keeping the JUnit
// reporter (cmd/w3ctest) simple and mirroring the xsd11/xslt30 suites.
var qt3AllCases []qt3Test

func TestQT3W3C(t *testing.T) {
	// Guard the hand-authored expectations before the expensive run so a bare
	// conformance run (cmd/w3ctest) also fails fast on a skip/xfail entry that
	// names no runnable case — otherwise a stale/typoed xfail would silently
	// never exercise the unexpected-pass tripwire.
	for _, p := range qt3ValidateExpectations(qt3LoadExpectations(), qt3AllCases) {
		t.Fatalf("expectations/qt3.json: %s", p)
	}
	// qt3RunTests skips when fixtures are unfetched.
	qt3RunTests(t, qt3AllCases)
}

// qt3RepoTestDataDir resolves the absolute testdata/qt3ts directory from this
// package's source location, independent of the working directory.
func qt3RepoTestDataDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("..", "testdata", "qt3ts")
	}
	// file is <root>/xpath3/qt3_shims_test.go → <root>/testdata/qt3ts
	return filepath.Join(filepath.Dir(filepath.Dir(file)), "testdata", "qt3ts")
}

// qt3HTTPResolver resolves http(s) URIs for QT3 resource/collection tests via
// the harness's shared fixture server. It implements xpath3.URIResolver.
type qt3HTTPResolver struct {
	client *http.Client
}

func (r *qt3HTTPResolver) ResolveURI(uri string) (io.ReadCloser, error) {
	resp, err := r.client.Get(uri) //nolint:noctx,gosec // harness fetches its own fixture server
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http %d resolving %q", resp.StatusCode, uri)
	}
	return resp.Body, nil
}

// qt3FileURIResolver resolves file:// URIs and relative/absolute file paths for
// the harness. It implements xpath3.URIResolver and mirrors the upstream helium
// resolver's cross-platform semantics. baseDir is "/" in practice, so no extra
// confinement is applied beyond opening the resolved path.
type qt3FileURIResolver struct {
	baseDir string
}

func (r *qt3FileURIResolver) ResolveURI(uri string) (io.ReadCloser, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	var target string
	switch {
	case parsed.Scheme == "file":
		p, ferr := qt3FileURIToPath(parsed, uri)
		if ferr != nil {
			return nil, ferr
		}
		target = p
	case parsed.Scheme == "" || qt3IsWindowsDriveScheme(parsed):
		if filepath.IsAbs(uri) || qt3IsWindowsAbsolute(uri) || strings.HasPrefix(uri, "/") {
			target = uri
		} else {
			target = filepath.Join(r.baseDir, uri)
		}
	default:
		return nil, fmt.Errorf("unsupported URI scheme: %s", parsed.Scheme)
	}
	return os.Open(target) //nolint:gosec // harness reads test fixtures by path
}

// qt3FileURIToPath converts a "file:" URI to a native path: reject non-local
// hosts, opaque/empty paths and UNC forms, and on Windows strip the leading
// slash before a drive letter ("file:///C:/a" -> "C:\a").
func qt3FileURIToPath(parsed *url.URL, uri string) (string, error) {
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("non-local file URI host %q in %q", parsed.Host, uri)
	}
	if parsed.Opaque != "" || parsed.Path == "" {
		return "", fmt.Errorf("invalid file URI %q: no local path", uri)
	}
	p := parsed.Path
	if len(p) >= 2 && qt3IsPathSep(p[0]) && qt3IsPathSep(p[1]) {
		return "", fmt.Errorf("UNC file URI %q is not a local path", uri)
	}
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && qt3IsASCIILetter(p[1]) && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

func qt3IsWindowsDriveScheme(parsed *url.URL) bool {
	return len(parsed.Scheme) == 1 && qt3IsASCIILetter(parsed.Scheme[0])
}

// qt3IsWindowsAbsolute reports whether p is a Windows-absolute path ("C:\...",
// "C:/...", or a leading backslash), regardless of host OS.
func qt3IsWindowsAbsolute(p string) bool {
	if len(p) >= 3 && qt3IsASCIILetter(p[0]) && p[1] == ':' && qt3IsPathSep(p[2]) {
		return true
	}
	return len(p) > 0 && p[0] == '\\'
}

// qt3WindowsToFileURI converts a Windows absolute path to a "file:" URI,
// mirroring uripath.WindowsToFileURI: "file:///C:/a/b" for a drive path,
// "file://server/share" for a UNC path.
func qt3WindowsToFileURI(s string) string {
	slashed := strings.ReplaceAll(s, "\\", "/")
	if len(s) >= 2 && qt3IsASCIILetter(s[0]) && s[1] == ':' {
		return "file:///" + slashed
	}
	return "file:" + slashed
}

func qt3IsASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func qt3IsPathSep(b byte) bool { return b == '/' || b == '\\' }
