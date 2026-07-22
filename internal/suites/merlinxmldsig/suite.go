// Package merlinxmldsig generates the conformance case tables for the 2002
// IETF/W3C baseline "merlin-xmldsig-twenty-three" sample-signature collection
// (the Baltimore/Merlin Hughes vectors the two newer xmldsig suites build on).
//
// The collection ships inside the same Apache Santuario checkout the xmldsig11
// suite pins, so this suite reuses that clone (a shared sourceDir, like
// xsd10/xsd11). Fetch clones/checks out the shared source if needed, then copies
// the signed documents and certs into the gitignored testdata/merlinxmldsig.
// Generate enumerates the signed documents (sorted, deterministic); when the
// clone is absent it emits an empty case table (per-case skips cover absence).
package merlinxmldsig

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

// collectionSubdir is the merlin collection inside the santuario checkout.
const collectionSubdir = "src/test/resources/ie/baltimore/merlin-examples/merlin-xmldsig-twenty-three"

// truncatedHMACCase is the one signed document whose signature is deliberately
// invalid (a 40-bit-truncated HMACOutputLength): verification MUST fail.
const truncatedHMACCase = "signature-enveloping-hmac-sha1-40"

type Suite struct{}

func New() Suite {
	return Suite{}
}

func (Suite) Name() string {
	return "merlinxmldsig"
}

// Fetch ensures the shared santuario clone is present, then copies the merlin
// signed documents and certs into testdata/merlinxmldsig.
func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	if err := generator.FetchSource(ctx, genCtx.Root, s.Name(), suiteLock); err != nil {
		return err
	}
	collectionRoot := s.collectionRoot(genCtx.Root, suiteLock)
	rels, err := copyRels(collectionRoot)
	if err != nil {
		return err
	}
	destRoot := filepath.Join(genCtx.Root, "testdata", "merlinxmldsig")
	for _, rel := range rels {
		src, err := generator.ContainedPath(collectionRoot, rel)
		if err != nil {
			return fmt.Errorf("resolve fixture %q: %w", rel, err)
		}
		dst, err := generator.ContainedPath(destRoot, rel)
		if err != nil {
			return fmt.Errorf("resolve fixture dest %q: %w", rel, err)
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	fmt.Printf("merlinxmldsig: copied %d files into testdata/merlinxmldsig\n", len(rels))
	return nil
}

func (s Suite) collectionRoot(root string, suiteLock generator.SuiteLock) string {
	return filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir), filepath.FromSlash(collectionSubdir))
}

// signedDocs returns the signed-document filenames (signature*.xml minus the
// signature.tmpl template) as sorted slash paths relative to the collection root.
func signedDocs(collectionRoot string) ([]string, error) {
	entries, err := os.ReadDir(collectionRoot)
	if err != nil {
		return nil, fmt.Errorf("read merlin collection: %w", err)
	}
	var docs []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "signature") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		docs = append(docs, name)
	}
	sort.Strings(docs)
	return docs, nil
}

// copyRels returns every file the harness needs: the signed documents plus the
// certs directory (cert material for the KeyName/X509 cases).
func copyRels(collectionRoot string) ([]string, error) {
	docs, err := signedDocs(collectionRoot)
	if err != nil {
		return nil, err
	}
	rels := append([]string{}, docs...)

	certsDir := filepath.Join(collectionRoot, "certs")
	certEntries, err := os.ReadDir(certsDir)
	if err != nil {
		return nil, fmt.Errorf("read merlin certs: %w", err)
	}
	for _, e := range certEntries {
		if e.IsDir() {
			continue
		}
		rels = append(rels, "certs/"+e.Name())
	}
	sort.Strings(rels)
	return rels, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create fixture dir for %s: %w", dst, err)
	}
	in, err := os.Open(src) //nolint:gosec // src is contained under the santuario checkout
	if err != nil {
		return fmt.Errorf("open fixture %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst) //nolint:gosec // dst is contained under testdata/merlinxmldsig
	if err != nil {
		return fmt.Errorf("create fixture %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy fixture %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close fixture %s: %w", dst, err)
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
		docs, err := signedDocs(s.collectionRoot(genCtx.Root, suiteLock))
		if err != nil {
			return err
		}
		for _, name := range docs {
			id := strings.TrimSuffix(name, ".xml")
			cases = append(cases, genCase{
				ID:       id,
				File:     name,
				MustFail: id == truncatedHMACCase,
			})
		}
	}

	source := casesSource(cases)
	out := filepath.Join(genCtx.Root, "xmldsig", "merlinxmldsig_cases_gen_test.go")
	return generator.WriteGoFile(out, source, mode)
}

type genCase struct {
	ID       string
	File     string
	MustFail bool
}

func casesSource(cases []genCase) string {
	var b strings.Builder
	b.WriteString("// Code generated by w3cgen; DO NOT EDIT.\n\n")
	b.WriteString("package xmldsig_test\n\n")
	b.WriteString("var merlinXMLDSigCases = []merlinCase{\n")
	for _, c := range cases {
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(c.ID))
		fmt.Fprintf(&b, "\t\tFile: %s,\n", strconv.Quote(c.File))
		if c.MustFail {
			b.WriteString("\t\tMustFail: true,\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}
