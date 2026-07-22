package xmldsig_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xmldsig1"
)

// containedPath joins root and a testdata-relative slash path, rejecting an
// absolute or escaping rel. Case @Input/@Output values come from committed
// fixtures, but the runner refuses to read outside the testdata root regardless.
func containedPath(root, rel string) (string, bool) {
	clean := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	r, err := filepath.Rel(root, clean)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

// confinedFS is the FS helium resolves any external reference through. Those
// paths are fixture-controlled, so it rejects any name escaping the testdata
// root before delegating to os.DirFS.
type confinedFS struct {
	root string
	fsys fs.FS
}

func newConfinedFS(root string) confinedFS {
	return confinedFS{root: root, fsys: os.DirFS(root)}
}

func (c confinedFS) Open(name string) (fs.File, error) {
	slashed := filepath.ToSlash(name)
	if _, ok := containedPath(c.root, slashed); !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return c.fsys.Open(slashed)
}

// expectations is the {skip, xfail} metadata shared by both suites (same shape
// and xfail-inversion semantics as the xml suite).
type expectations struct {
	Skip  map[string]string `json:"skip"`
	XFail map[string]string `json:"xfail"`
}

func loadExpectations(t *testing.T, envVar, name string) expectations {
	t.Helper()
	p := os.Getenv(envVar)
	if p == "" {
		p = filepath.Join(harness.RepoRoot(t), "expectations", name)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read expectations %s: %v", p, err)
	}
	var exp expectations
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("parse expectations %s: %v", p, err)
	}
	return exp
}

// outcome routes a case's conformance-signal assertions. For an ordinary case,
// errorf forwards to t.Errorf. For an xfail case it collects the messages so the
// root test can invert them. Infrastructure problems (missing fixtures) stay on
// t directly via t.Skipf/t.Fatalf and are never inverted.
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

// runCase applies the shared skip / xfail-inversion policy around fn. fn reports
// the conformance signal through the outcome; an xfail case passes when fn
// reports at least one failure and fails loudly (telling you to drop the entry)
// when it unexpectedly conforms.
func runCase(t *testing.T, exp expectations, id string, fn func(o *outcome)) {
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
		t.Errorf("XFAIL %s unexpectedly PASSED — helium now conforms; remove it from expectations xfail (%s)", id, xreason)
		return
	}
	t.Logf("xfail (%s): %s", xreason, strings.Join(o.fails, "; "))
}

// keySource builds a KeySource for the interop vectors: HMAC signatures use the
// suite's literal secret; asymmetric signatures resolve a public key from the
// signature's inline KeyInfo (X509Certificate, RSAKeyValue, or ECKeyValue).
func keySource(secret []byte) xmldsig1.KeySource {
	return keyResolver{secret: secret}
}

type keyResolver struct {
	secret []byte
}

func (k keyResolver) ResolveKey(_ context.Context, keyInfo *xmldsig1.KeyInfoData, alg string) (any, error) {
	if strings.Contains(alg, "hmac") {
		return k.secret, nil
	}
	if keyInfo == nil {
		return nil, fmt.Errorf("no KeyInfo to resolve key for %s", alg)
	}
	if len(keyInfo.X509Certificates) > 0 {
		return keyInfo.X509Certificates[0].PublicKey, nil
	}
	if keyInfo.RSAKeyValue != nil {
		return &rsa.PublicKey{N: keyInfo.RSAKeyValue.Modulus, E: keyInfo.RSAKeyValue.Exponent}, nil
	}
	if keyInfo.ECKeyValue != nil {
		return &ecdsa.PublicKey{Curve: keyInfo.ECKeyValue.Curve, X: keyInfo.ECKeyValue.X, Y: keyInfo.ECKeyValue.Y}, nil
	}
	return nil, fmt.Errorf("no supported key material in KeyInfo for %s", alg)
}

// signedDocParser parses a signed document faithfully for verification: no entity
// substitution or DTD validation, external references confined to the testdata
// root, XXE unblocked so any inline DOCTYPE parses.
func signedDocParser(testdataRoot string) helium.Parser {
	return helium.NewParser().
		BlockXXE(false).
		FS(newConfinedFS(testdataRoot))
}
