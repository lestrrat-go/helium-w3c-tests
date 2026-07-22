package xmldsig_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/c14n"
	"github.com/lestrrat-go/helium/xmldsig1"
	"github.com/lestrrat-go/helium/xpath1"
)

// dsig2edC14NCase is a pure Canonical XML 1.1 node-set case. The runner
// evaluates XPath over Input, canonicalizes the resulting node-set with C14N
// 1.1, and byte-compares to Output. Populated by the generated case table.
type dsig2edC14NCase struct {
	ID     string
	Input  string
	XPath  string
	Output string
}

// dsig2edSigCase is a signature-verification case (expected verdict: valid).
type dsig2edSigCase struct {
	ID    string
	Group string
	Input string
}

// c14nNamespaces are the prefix bindings the c14n11 XPath node-set expressions
// use. Every input document binds ietf and w3c to these URIs; unprefixed names
// in the expressions (spec3) resolve to no namespace, matching those inputs.
var c14nNamespaces = map[string]string{
	"ietf": "http://www.ietf.org",
	"w3c":  "http://www.w3.org",
}

// hmacSecretXMLDSig2Ed is the HMAC key the Note specifies: the ASCII bytes of
// the word "secret".
var hmacSecretXMLDSig2Ed = []byte("secret")

func TestXMLDSig2EdW3C(t *testing.T) {
	exp := loadExpectations(t, "XMLDSIG2ED_EXPECTATIONS", "xmldsig2ed.json")
	testdataRoot := harness.SourceDir(t, "testdata/xmldsig2ed")

	// The dname cases carry only an X509 Distinguished Name in KeyInfo, so the
	// resolver selects the signing cert from the vendored certs pool out of band.
	certs := loadCerts(t, filepath.Join(testdataRoot, "xmldsig", "dname", "certs"))
	sigKeySource := keySourceWithCerts(hmacSecretXMLDSig2Ed, certs)

	for _, c := range xmldsig2edC14NCases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runC14NCase(t, o, testdataRoot, c)
			})
		})
	}
	for _, c := range xmldsig2edSigCases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runSigCase(t, o, testdataRoot, c.ID, c.Input, sigKeySource)
			})
		})
	}
}

func runC14NCase(t *testing.T, o *outcome, root string, c dsig2edC14NCase) {
	t.Helper()
	defer recoverAsFailure(o, c.ID)

	inputPath := mustContained(t, root, c.Input)
	src := readFixture(t, inputPath, "xmldsig2ed")
	xpathPath := mustContained(t, root, c.XPath)
	exprBytes := readFixture(t, xpathPath, "xmldsig2ed")
	outputPath := mustContained(t, root, c.Output)
	want := readFixture(t, outputPath, "xmldsig2ed")

	// The internal DTD subset of some inputs declares ID-typed attributes that
	// the id() calls in the node-set expressions rely on, so DTD attributes are
	// processed (external DTD loading confined to the testdata root).
	doc, err := helium.NewParser().
		BlockXXE(false).
		LoadExternalDTD(true).
		DefaultDTDAttributes(true).
		SubstituteEntities(true).
		FS(newConfinedFS(root)).
		BaseURI(c.Input).
		Parse(t.Context(), src)
	if err != nil {
		o.errorf("%s: parse input: %v", c.ID, err)
		return
	}

	compiled, err := xpath1.Compile(strings.TrimSpace(string(exprBytes)))
	if err != nil {
		o.errorf("%s: compile xpath: %v", c.ID, err)
		return
	}
	result, err := xpath1.NewEvaluator().Namespaces(c14nNamespaces).Evaluate(t.Context(), compiled, doc)
	if err != nil {
		o.errorf("%s: evaluate xpath: %v", c.ID, err)
		return
	}
	if result.Type != xpath1.NodeSetResult {
		o.errorf("%s: xpath produced %v, want a node-set", c.ID, result.Type)
		return
	}

	got, err := c14n.NewCanonicalizer(c14n.C14N11).NodeSet(result.NodeSet).CanonicalizeTo(doc)
	if err != nil {
		o.errorf("%s: canonicalize: %v", c.ID, err)
		return
	}
	if !bytes.Equal(got, want) {
		o.errorf("%s: canonical output mismatch\n got: %q\nwant: %q", c.ID, got, want)
	}
}

func runSigCase(t *testing.T, o *outcome, root, id, input string, ks xmldsig1.KeySource) {
	t.Helper()
	defer recoverAsFailure(o, id)

	docPath := mustContained(t, root, input)
	src := readFixture(t, docPath, "xmldsig2ed")

	doc, err := signedDocParser(root).BaseURI(input).Parse(t.Context(), src)
	if err != nil {
		o.errorf("%s: parse signed document: %v", id, err)
		return
	}
	if _, err := xmldsig1.NewVerifier(ks).AllowSHA1(true).Verify(t.Context(), doc); err != nil {
		o.errorf("%s: verification failed: %v", id, err)
	}
}

// mustContained resolves a testdata-relative path or fails the test as an
// infrastructure error (a bad generated path, not a conformance signal).
func mustContained(t *testing.T, root, rel string) string {
	t.Helper()
	p, ok := containedPath(root, rel)
	if !ok {
		t.Fatalf("path %q escapes the testdata root", rel)
	}
	return p
}

// readFixture reads a fixture file, skipping the case (not failing) when the
// testdata tree has not been fetched yet.
func readFixture(t *testing.T, path, suite string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch %s (missing %s)", suite, path)
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// recoverAsFailure converts a panic in a case into a conformance failure so a
// crash in one case never aborts the suite (and an xfail case's panic counts as
// its expected failure).
func recoverAsFailure(o *outcome, id string) {
	if r := recover(); r != nil {
		o.errorf("%s: panic: %v", id, r)
	}
}
