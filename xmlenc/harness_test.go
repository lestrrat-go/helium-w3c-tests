package xmlenc_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
)

type expectations struct {
	Skip  map[string]string `json:"skip"`
	XFail map[string]string `json:"xfail"`
}

func loadExpectations(t *testing.T) expectations {
	t.Helper()
	path := os.Getenv("XMLENC11_EXPECTATIONS")
	if path == "" {
		path = filepath.Join(harness.RepoRoot(t), "expectations", "xmlenc11.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read expectations %s: %v", path, err)
	}
	var exp expectations
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("parse expectations %s: %v", path, err)
	}
	return exp
}

type outcome struct {
	t     *testing.T
	xfail bool
	fails []string
}

func (o *outcome) errorf(format string, args ...any) {
	if o.xfail {
		o.fails = append(o.fails, fmt.Sprintf(format, args...))
		return
	}
	o.t.Errorf(format, args...)
}

func runCase(t *testing.T, exp expectations, id string, fn func(*outcome)) {
	t.Helper()
	if reason := exp.Skip[id]; reason != "" {
		t.Skip(reason)
	}
	xreason, isXFail := exp.XFail[id]
	o := &outcome{t: t, xfail: isXFail}
	fn(o)
	if !isXFail {
		return
	}
	if len(o.fails) == 0 {
		t.Errorf("XFAIL %s unexpectedly PASSED — remove it from expectations xfail (%s)", id, xreason)
		return
	}
	t.Logf("xfail (%s): %s", xreason, strings.Join(o.fails, "; "))
}

func containedPath(root, rel string) (string, bool) {
	clean := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	r, err := filepath.Rel(root, clean)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func mustContained(t *testing.T, root, rel string) string {
	t.Helper()
	p, ok := containedPath(root, rel)
	if !ok {
		t.Fatalf("path %q escapes the testdata root", rel)
	}
	return p
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch xmlenc11 (missing %s)", path)
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func recoverAsFailure(o *outcome, id string) {
	if r := recover(); r != nil {
		o.errorf("%s: panic: %v", id, r)
	}
}
