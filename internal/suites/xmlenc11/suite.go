// Package xmlenc11 generates the conformance case table for the W3C XML
// Encryption 1.1 core interoperability vectors.
//
// The source artifacts are pinned to the Apache Santuario checkout already
// used by the XMLDSig 1.1 suite. Fetch copies the XML vectors and key material
// into the gitignored testdata/xmlenc11 tree; generate enumerates the vectors
// deterministically from that pinned source.
package xmlenc11

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

const vectorSubdir = "src/test/resources/org/w3c/www/interop/xmlenc-core-11"

type Suite struct{}

func New() Suite {
	return Suite{}
}

func (Suite) Name() string {
	return "xmlenc11"
}

func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	if err := generator.FetchSource(ctx, genCtx.Root, s.Name(), suiteLock); err != nil {
		return err
	}

	sourceRoot := filepath.Join(genCtx.Root, filepath.FromSlash(suiteLock.SourceDir), filepath.FromSlash(vectorSubdir))
	files, err := artifactFiles(sourceRoot)
	if err != nil {
		return err
	}
	destRoot := filepath.Join(genCtx.Root, "testdata", "xmlenc11")
	for _, rel := range files {
		src, err := generator.ContainedPath(sourceRoot, rel)
		if err != nil {
			return fmt.Errorf("resolve artifact %q: %w", rel, err)
		}
		dst, err := generator.ContainedPath(destRoot, rel)
		if err != nil {
			return fmt.Errorf("resolve artifact destination %q: %w", rel, err)
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	fmt.Printf("xmlenc11: copied %d artifacts into testdata/xmlenc11\n", len(files))
	return nil
}

func artifactFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read XML Encryption 1.1 artifacts: %w", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "plaintext.xml" || name == "binary-data.hex" ||
			(strings.HasPrefix(name, "cipherText") && strings.HasSuffix(name, ".xml")) ||
			strings.HasSuffix(name, ".p12") || strings.HasSuffix(name, ".pfx") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create artifact directory for %s: %w", dst, err)
	}
	in, err := os.Open(src) //nolint:gosec // src is contained under the pinned checkout
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst) //nolint:gosec // dst is contained under testdata/xmlenc11
	if err != nil {
		return fmt.Errorf("create artifact %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy artifact %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close artifact %s: %w", dst, err)
	}
	return nil
}

func (s Suite) Generate(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock, mode generator.GenerateMode) error {
	_ = ctx
	sourceDir := filepath.Join(genCtx.Root, filepath.FromSlash(suiteLock.SourceDir))
	var cases []genCase
	if _, err := os.Stat(sourceDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", suiteLock.SourceDir, err)
		}
	} else {
		files, err := vectorFiles(filepath.Join(sourceDir, filepath.FromSlash(vectorSubdir)))
		if err != nil {
			return err
		}
		for _, rel := range files {
			name := strings.TrimSuffix(filepath.Base(rel), ".xml")
			cases = append(cases, genCase{
				ID:          name,
				File:        rel,
				KeyFile:     keyFile(name),
				KeyPassword: keyPassword(name),
				Binary:      binaryVector(name),
			})
		}
	}

	return generator.WriteGoFile(
		filepath.Join(genCtx.Root, "xmlenc", "xmlenc11_cases_gen_test.go"),
		casesSource(cases),
		mode,
	)
}

func vectorFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read XML Encryption 1.1 vectors: %w", err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "cipherText") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

func keyFile(id string) string {
	switch {
	case strings.HasPrefix(id, "cipherText__RSA-2048__"):
		return "RSA-2048_SHA256WithRSA.p12"
	case strings.HasPrefix(id, "cipherText__RSA-3072__"):
		return "RSA-3072_SHA256WithRSA.p12"
	case strings.HasPrefix(id, "cipherText__RSA-4096__"):
		return "RSA-4096_SHA256WithRSA.p12"
	case strings.HasPrefix(id, "cipherText__EC-P256__") && strings.HasSuffix(id, "__ConcatKDF-1"):
		return "EC-P256_SHA256WithECDSA-v02.p12"
	case strings.HasPrefix(id, "cipherText__EC-P384__") && strings.HasSuffix(id, "__ConcatKDF-2"):
		return "EC-P384_SHA256WithECDSA-v02.p12"
	case strings.HasPrefix(id, "cipherText__EC-P521__") && strings.HasSuffix(id, "__ConcatKDF-3"):
		return "EC-P521_SHA256WithECDSA-v02.p12"
	case strings.HasPrefix(id, "cipherText__EC-P256__") && strings.HasSuffix(id, "__ConcatKDF-4"):
		return "EC-P256.pfx"
	case strings.HasPrefix(id, "cipherText__EC-P384__") && strings.HasSuffix(id, "__ConcatKDF-5"):
		return "EC-P384.pfx"
	case strings.HasPrefix(id, "cipherText__EC-P521__") && strings.HasSuffix(id, "__ConcatKDF-6"):
		return "EC-P521.pfx"
	}
	return ""
}

func keyPassword(id string) string {
	if strings.HasSuffix(id, "__ConcatKDF-4") ||
		strings.HasSuffix(id, "__ConcatKDF-5") ||
		strings.HasSuffix(id, "__ConcatKDF-6") {
		return "1234"
	}
	return "passwd"
}

func binaryVector(id string) bool {
	return strings.HasSuffix(id, "__ConcatKDF-4") ||
		strings.HasSuffix(id, "__ConcatKDF-5") ||
		strings.HasSuffix(id, "__ConcatKDF-6")
}

type genCase struct {
	ID          string
	File        string
	KeyFile     string
	KeyPassword string
	Binary      bool
}

func casesSource(cases []genCase) string {
	var b strings.Builder
	b.WriteString("// Code generated by w3cgen; DO NOT EDIT.\n\n")
	b.WriteString("package xmlenc_test\n\n")
	b.WriteString("var xmlenc11Cases = []xmlenc11Case{\n")
	for _, c := range cases {
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(c.ID))
		fmt.Fprintf(&b, "\t\tFile: %s,\n", strconv.Quote(c.File))
		if c.KeyFile != "" {
			fmt.Fprintf(&b, "\t\tKeyFile: %s,\n", strconv.Quote(c.KeyFile))
			fmt.Fprintf(&b, "\t\tKeyPassword: %s,\n", strconv.Quote(c.KeyPassword))
		}
		if c.Binary {
			b.WriteString("\t\tBinary: true,\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}
