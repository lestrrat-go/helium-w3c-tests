package xmldsig_test

import (
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
)

// dsig11Case is one XMLDSig 1.1 enveloping-signature vector (expected verdict:
// valid). File is a testdata/xmldsig11-relative slash path. Populated by the
// generated case table.
type dsig11Case struct {
	ID   string
	File string
}

// hmacSecretXMLDSig11 is the HMAC key the santuario interop driver uses for the
// xmldsig11 vectors: the ASCII bytes of "testkey".
var hmacSecretXMLDSig11 = []byte("testkey")

func TestXMLDSig11W3C(t *testing.T) {
	exp := loadExpectations(t, "XMLDSIG11_EXPECTATIONS", "xmldsig11.json")
	testdataRoot := harness.SourceDir(t, "testdata/xmldsig11")

	for _, c := range xmldsig11Cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runSigCase(t, o, testdataRoot, c.ID, c.File, hmacSecretXMLDSig11)
			})
		})
	}
}
