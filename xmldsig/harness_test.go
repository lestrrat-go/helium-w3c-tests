package xmldsig_test

import (
	"context"
	"crypto/dsa" //nolint:staticcheck // DSA is required to verify the 2002 baseline DSA-SHA1 interop vectors
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

// keySourceWithCerts extends keySource with an out-of-band certificate set the
// resolver selects from when the KeyInfo carries only an X509 Distinguished Name
// reference (X509SubjectName / X509IssuerSerial) rather than a full certificate.
func keySourceWithCerts(secret []byte, certs []*x509.Certificate) xmldsig1.KeySource {
	return keyResolver{secret: secret, certs: certs}
}

type keyResolver struct {
	secret []byte
	// certs is the out-of-band certificate pool for DName-only KeyInfo (the
	// xmldsig2ed dname cases). Empty for suites whose KeyInfo carries the key.
	certs []*x509.Certificate
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
	if keyInfo.DSAKeyValue != nil {
		v := keyInfo.DSAKeyValue
		return &dsa.PublicKey{Parameters: dsa.Parameters{P: v.P, Q: v.Q, G: v.G}, Y: v.Y}, nil
	}
	if cert := k.matchDNameCert(keyInfo); cert != nil {
		return cert.PublicKey, nil
	}
	if cert := k.matchIssuerSerialCert(keyInfo); cert != nil {
		return cert.PublicKey, nil
	}
	return nil, fmt.Errorf("no supported key material in KeyInfo for %s", alg)
}

// matchDNameCert resolves a DName-only KeyInfo (X509SubjectName, the form the
// xmldsig2ed dname cases use) to a vendored certificate.
//
// Division of labor: helium's keyinfo parser surfaces the ds:X509SubjectName
// string VERBATIM and performs NO DName canonicalization or matching. The
// HARNESS decodes that RFC 2253/4514 string into attribute VALUES and compares
// them to each candidate certificate's Subject, which Go's crypto/x509 has
// already decoded. Matching on decoded values (not on the escaped string) is
// what lets these cases pass: the very thing they test is that the same subject
// is written with different escaping (e.g. "Trailing\20\20" vs "Trailing \ "),
// which decodes to the same value but is not byte-equal as a string.
func (k keyResolver) matchDNameCert(keyInfo *xmldsig1.KeyInfoData) *x509.Certificate {
	for _, subjectName := range keyInfo.X509SubjectNames {
		want := decodeDNameCN(subjectName)
		if want == "" {
			continue
		}
		for _, cert := range k.certs {
			if cert.Subject.CommonName == want {
				return cert
			}
		}
	}
	return nil
}

// matchIssuerSerialCert resolves an X509IssuerSerial-only KeyInfo (the merlin
// signature-x509-is case) to a vendored certificate. Helium surfaces the issuer
// DName and serial VERBATIM (no matching); the harness selects the cert by
// serial number, then confirms the issuer CN matches after decoding, so a stray
// serial collision cannot pick the wrong cert.
func (k keyResolver) matchIssuerSerialCert(keyInfo *xmldsig1.KeyInfoData) *x509.Certificate {
	for _, is := range keyInfo.X509IssuerSerials {
		if is.SerialNumber == nil {
			continue
		}
		wantIssuerCN := decodeDNameCN(is.IssuerName)
		for _, cert := range k.certs {
			if cert.SerialNumber == nil || cert.SerialNumber.Cmp(is.SerialNumber) != 0 {
				continue
			}
			if wantIssuerCN == "" || cert.Issuer.CommonName == wantIssuerCN {
				return cert
			}
		}
	}
	return nil
}

// decodeDNameCN extracts and unescapes the CN attribute value from an RFC
// 2253/4514 distinguished name string. It splits RDNs on unescaped commas,
// finds the CN attribute, and decodes its value's backslash escapes (\XX hex
// pairs and \<char> single-character escapes). Returns "" if there is no CN.
func decodeDNameCN(dname string) string {
	for _, rdn := range splitUnescaped(strings.TrimSpace(dname), ',') {
		eq := indexUnescaped(rdn, '=')
		if eq < 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rdn[:eq]), "CN") {
			continue
		}
		return unescapeDNameValue(rdn[eq+1:])
	}
	return ""
}

// splitUnescaped splits s on every occurrence of sep not preceded by a
// backslash escape.
func splitUnescaped(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// indexUnescaped returns the index of the first unescaped sep, or -1.
func indexUnescaped(s string, sep byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == sep {
			return i
		}
	}
	return -1
}

// unescapeDNameValue decodes RFC 2253/4514 escapes in an attribute value: a
// backslash followed by two hex digits is one byte; a backslash followed by any
// other character is that character literally.
func unescapeDNameValue(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 >= len(v) {
			b.WriteByte(v[i])
			continue
		}
		if i+2 < len(v) && isHex(v[i+1]) && isHex(v[i+2]) {
			b.WriteByte(hexVal(v[i+1])<<4 | hexVal(v[i+2]))
			i += 2
			continue
		}
		b.WriteByte(v[i+1])
		i++
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// loadCerts parses every .crt (PEM or DER) certificate in dir. Returns an empty
// slice when the directory is absent (fixtures not fetched).
func loadCerts(t *testing.T, dir string) []*x509.Certificate {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read certs dir %s: %v", dir, err)
	}
	var certs []*x509.Certificate
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read cert %s: %v", e.Name(), err)
		}
		cert := parseCert(t, e.Name(), data)
		if cert != nil {
			certs = append(certs, cert)
		}
	}
	return certs
}

func parseCert(t *testing.T, name string, data []byte) *x509.Certificate {
	t.Helper()
	der := data
	if block, _ := pem.Decode(data); block != nil {
		der = block.Bytes
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert %s: %v", name, err)
	}
	return cert
}

// signedDocParser parses a signed document faithfully for verification: no entity
// substitution or DTD validation, external references confined to the testdata
// root, XXE unblocked so any inline DOCTYPE parses.
func signedDocParser(testdataRoot string) helium.Parser {
	return helium.NewParser().
		BlockXXE(false).
		FS(newConfinedFS(testdataRoot))
}
