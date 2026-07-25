package xmlenc_test

import (
	"crypto/rsa"
	"fmt"
	"path/filepath"
	"testing"

	helium "github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xmlenc1"
	"software.sslmate.com/src/go-pkcs12"
)

type xmlenc11Case struct {
	ID      string
	File    string
	KeyFile string
}

func TestXMLEnc11W3C(t *testing.T) {
	exp := loadExpectations(t)
	testdataRoot := harness.SourceDir(t, "testdata/xmlenc11")
	for _, c := range xmlenc11Cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			runCase(t, exp, c.ID, func(o *outcome) {
				runXMLEncCase(t, o, testdataRoot, c)
			})
		})
	}
}

func runXMLEncCase(t *testing.T, o *outcome, root string, c xmlenc11Case) {
	t.Helper()
	defer recoverAsFailure(o, c.ID)

	if c.KeyFile == "" {
		o.errorf("%s: generated case has no decryption key", c.ID)
		return
	}
	vectorPath := mustContained(t, root, c.File)
	src := readFixture(t, vectorPath)
	doc, err := helium.NewParser().Parse(t.Context(), src)
	if err != nil {
		o.errorf("%s: parse encrypted document: %v", c.ID, err)
		return
	}

	keyPath := mustContained(t, root, filepath.Base(c.KeyFile))
	keyData := readFixture(t, keyPath)
	privateKey, err := decodeRSAKey(keyData)
	if err != nil {
		o.errorf("%s: decode PKCS#12 key %s: %v", c.ID, c.KeyFile, err)
		return
	}

	nodes, err := xmlenc1.NewDecryptor().PrivateKey(privateKey).Decrypt(t.Context(), doc.DocumentElement())
	if err != nil {
		o.errorf("%s: decrypt: %v", c.ID, err)
		return
	}
	if len(nodes) != 1 {
		o.errorf("%s: decrypted node count = %d, want 1", c.ID, len(nodes))
		return
	}

	plainPath := mustContained(t, root, "plaintext.xml")
	plainDoc, err := helium.NewParser().Parse(t.Context(), readFixture(t, plainPath))
	if err != nil {
		o.errorf("%s: parse plaintext: %v", c.ID, err)
		return
	}
	got, err := helium.WriteString(nodes[0])
	if err != nil {
		o.errorf("%s: serialize decrypted element: %v", c.ID, err)
		return
	}
	want, err := helium.WriteString(plainDoc.DocumentElement())
	if err != nil {
		o.errorf("%s: serialize plaintext element: %v", c.ID, err)
		return
	}
	if got != want {
		o.errorf("%s: decrypted element differs from plaintext\n got: %q\nwant: %q", c.ID, got, want)
	}
}

func decodeRSAKey(data []byte) (*rsa.PrivateKey, error) {
	key, _, err := pkcs12.Decode(data, "passwd")
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("decoded key has type %T, want *rsa.PrivateKey", key)
	}
	return rsaKey, nil
}
