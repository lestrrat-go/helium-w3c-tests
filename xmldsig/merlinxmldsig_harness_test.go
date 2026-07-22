package xmldsig_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xmldsig1"
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

	for _, c := range merlinXMLDSigCases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runMerlinCase(t, o, testdataRoot, c, sigKeySource)
			})
		})
	}
}

func runMerlinCase(t *testing.T, o *outcome, root string, c merlinCase, ks xmldsig1.KeySource) {
	t.Helper()
	defer recoverAsFailure(o, c.ID)

	docPath := mustContained(t, root, c.File)
	src := readFixture(t, docPath, "merlinxmldsig")

	doc, err := signedDocParser(root).BaseURI(c.File).Parse(t.Context(), src)
	if err != nil {
		o.errorf("%s: parse signed document: %v", c.ID, err)
		return
	}
	_, verr := xmldsig1.NewVerifier(ks).AllowSHA1(true).Verify(t.Context(), doc)

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
