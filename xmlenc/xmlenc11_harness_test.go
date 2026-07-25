package xmlenc_test

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	helium "github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xmlenc1"
	"software.sslmate.com/src/go-pkcs12"
)

type xmlenc11Case struct {
	ID          string
	File        string
	KeyFile     string
	KeyPassword string
	Binary      bool
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
	privateKey, err := decodePrivateKey(keyData, c.KeyPassword)
	if err != nil {
		o.errorf("%s: decode PKCS#12 key %s: %v", c.ID, c.KeyFile, err)
		return
	}

	decryptor := xmlenc1.NewDecryptor()
	switch key := privateKey.(type) {
	case *rsa.PrivateKey:
		decryptor = decryptor.PrivateKey(key)
	case *ecdsa.PrivateKey:
		decryptor = decryptor.ECPrivateKey(key)
	default:
		o.errorf("%s: decoded key has unsupported type %T", c.ID, privateKey)
		return
	}
	if c.Binary {
		got, err := decryptor.DecryptBytes(t.Context(), doc.DocumentElement())
		if err != nil {
			o.errorf("%s: decrypt: %v", c.ID, err)
			return
		}
		want, err := hex.DecodeString(strings.TrimSpace(string(readFixture(t, mustContained(t, root, "binary-data.hex")))))
		if err != nil {
			o.errorf("%s: decode binary plaintext: %v", c.ID, err)
			return
		}
		if string(got) != string(want) {
			o.errorf("%s: decrypted bytes differ from binary-data.hex", c.ID)
		}
		return
	}

	nodes, err := decryptor.Decrypt(t.Context(), doc.DocumentElement())
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

func decodePrivateKey(data []byte, password string) (any, error) {
	if password == "" {
		password = "passwd"
	}
	key, _, err := pkcs12.Decode(data, password)
	if err != nil {
		return nil, err
	}
	return key, nil
}
