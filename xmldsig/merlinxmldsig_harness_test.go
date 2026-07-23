package xmldsig_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xmldsig1"
	"github.com/lestrrat-go/helium/xmldsig1/transform"
)

// merlinCase is one document from the 2002 merlin-xmldsig-twenty-three baseline
// collection. File is a testdata/merlinxmldsig-relative slash path. MustFail
// marks the deliberately-invalid signature (the 40-bit-truncated HMAC), whose
// expected verdict is INVALID rather than valid. Populated by the generated
// case table.
type merlinCase struct {
	ID       string
	File     string
	MustFail bool
}

// hmacSecretMerlin is the HMAC key the collection's Readme specifies: the ASCII
// bytes of "secret" (hex 73 65 63 72 65 74).
var hmacSecretMerlin = []byte("secret")

func TestMerlinXMLDSigW3C(t *testing.T) {
	exp := loadExpectations(t, "MERLINXMLDSIG_EXPECTATIONS", "merlinxmldsig.json")
	testdataRoot := harness.SourceDir(t, "testdata/merlinxmldsig")

	// Some cases carry a KeyName or an X509 DName reference in KeyInfo, so the
	// resolver selects the signing cert from the collection's certs pool out of
	// band; the basic DSA/RSA/HMAC cases carry their key inline (or use the
	// shared HMAC secret).
	certs := loadCerts(t, filepath.Join(testdataRoot, "certs"))
	sigKeySource := keySourceWithCerts(hmacSecretMerlin, certs)
	resolver := merlinReferenceResolver{root: testdataRoot}

	for _, c := range merlinXMLDSigCases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runMerlinCase(t, o, testdataRoot, c, sigKeySource, resolver)
			})
		})
	}
}

// merlinReferenceResolver maps the two absolute external-reference URLs the
// merlin vectors sign against to their vendored local copies; everything else
// stays fail-closed. This mirrors the interop setup: the vectors were signed
// against these two W3C documents, which Santuario vendors locally so
// verification needs no network. helium's FSReferenceResolver cannot serve them
// because it refuses scheme URIs (these are absolute http URLs), so the harness
// supplies an explicit URL-to-file map.
type merlinReferenceResolver struct {
	root string
}

func (r merlinReferenceResolver) ResolveReference(ctx context.Context, uri string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch uri {
	case "http://www.w3.org/TR/xml-stylesheet":
		return os.ReadFile(filepath.Join(r.root, "xml-stylesheet"))
	case "http://www.w3.org/Signature/2002/04/xml-stylesheet.b64":
		return os.ReadFile(filepath.Join(r.root, "xml-stylesheet.b64"))
	}
	// A ds:RetrievalMethod points at a certificate by a relative path (e.g.
	// "certs/balor.crt"); serve it from the collection root, confined there. A
	// scheme URI with no explicit mapping above stays fail-closed.
	p, ok := containedPath(r.root, uri)
	if !ok {
		return nil, fmt.Errorf("%w: merlin resolver has no mapping for %q", xmldsig1.ErrReferenceNotFound, uri)
	}
	return os.ReadFile(p)
}

func runMerlinCase(t *testing.T, o *outcome, root string, c merlinCase, ks xmldsig1.KeySource, resolver xmldsig1.ReferenceResolver) {
	t.Helper()
	defer recoverAsFailure(o, c.ID)

	docPath := mustContained(t, root, c.File)
	src := readFixture(t, docPath, "merlinxmldsig")

	doc, err := signedDocParser(root).BaseURI(c.File).Parse(t.Context(), src)
	if err != nil {
		o.errorf("%s: parse signed document: %v", c.ID, err)
		return
	}
	// The composite signature-* vectors exercise the full profile: a general
	// XPointer Reference, an XSLT transform, here() in an XPath transform, and a
	// Manifest. Enable each opt-in (they no-op on vectors that don't use them) so
	// the whole collection can verify with one verifier.
	_, verr := xmldsig1.NewVerifier(ks).
		AllowSHA1(true).
		AllowXPointer(true).
		ValidateManifests(true).
		XSLTTransformer(transform.XSLT{}).
		ReferenceResolver(resolver).
		Verify(t.Context(), doc)

	if !c.MustFail {
		if verr != nil {
			o.errorf("%s: verification failed: %v", c.ID, verr)
		}
		return
	}

	// Expected-INVALID case: the interop collection requires this signature to be
	// rejected (its HMACOutputLength truncates the MAC to 40 bits). helium rejects
	// any HMACOutputLength fail-closed, which satisfies the must-reject: the case
	// passes when verification returns an error mentioning the truncation
	// parameter, and fails loudly if the bad signature were accepted.
	if verr == nil {
		o.errorf("%s: expected verification to FAIL (truncated HMACOutputLength) but it succeeded", c.ID)
		return
	}
	if !strings.Contains(verr.Error(), "HMACOutputLength") {
		o.errorf("%s: verification failed but not for the expected reason: %v", c.ID, verr)
	}
}
