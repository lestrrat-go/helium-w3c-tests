// Package xmldsig2ed generates the conformance case tables for the W3C Note
// "Test Cases for C14N 1.1 and XMLDSig Interoperability" (10 June 2008).
//
// The vectors have no upstream archive, so the harness-consumed subset is
// vendored (committed) under fixtures/xmldsig2ed. This is a "manual" lock kind:
// Fetch overlays the committed fixtures into the gitignored testdata/xmldsig2ed
// (it does NOT call generator.FetchSource, which errors on the manual kind).
// Generate enumerates cases from the committed fixtures tree, so case tables are
// produced deterministically whether or not testdata has been populated yet.
package xmldsig2ed

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

// fixturesDir is the committed vector tree, relative to the module root.
const fixturesDir = "fixtures/xmldsig2ed"

type Suite struct{}

func New() Suite {
	return Suite{}
}

func (Suite) Name() string {
	return "xmldsig2ed"
}

// Fetch overlays the committed fixtures into testdata/xmldsig2ed. The manual
// lock kind has no network source, so this copy is the whole "fetch".
func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	_ = ctx
	srcRoot := filepath.Join(genCtx.Root, filepath.FromSlash(fixturesDir))
	destRoot := filepath.Join(genCtx.Root, filepath.FromSlash(suiteLock.SourceDir))
	rels, err := fixtureRels(srcRoot)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		src, err := generator.ContainedPath(srcRoot, rel)
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
	fmt.Printf("xmldsig2ed: overlaid %d fixtures into %s\n", len(rels), suiteLock.SourceDir)
	return nil
}

// fixtureRels returns every regular file under srcRoot as a slash path relative
// to srcRoot, excluding the provenance README.
func fixtureRels(srcRoot string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(srcRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "README.md" {
			return nil
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk fixtures %s: %w", srcRoot, err)
	}
	sort.Strings(rels)
	return rels, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create fixture dir for %s: %w", dst, err)
	}
	in, err := os.Open(src) //nolint:gosec // src is contained under fixtures/xmldsig2ed
	if err != nil {
		return fmt.Errorf("open fixture %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst) //nolint:gosec // dst is contained under testdata/xmldsig2ed
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
	fixturesRoot := filepath.Join(genCtx.Root, filepath.FromSlash(fixturesDir))
	c14nCases, err := enumerateC14NCases(fixturesRoot)
	if err != nil {
		return err
	}
	sigCases, err := enumerateSigCases(fixturesRoot)
	if err != nil {
		return err
	}

	// No suite-level manifest test is emitted: both xmldsig suites share the
	// xmldsig_test package, and generator.ManifestTestSource declares
	// fixed-named package-level consts (w3cSuiteName, ...) that would collide
	// between the two. Per-case skips already point at the fetch command when
	// testdata is absent.
	source := casesSource(c14nCases, sigCases)
	out := filepath.Join(genCtx.Root, "xmldsig", "xmldsig2ed_cases_gen_test.go")
	return generator.WriteGoFile(out, source, mode)
}

// c14nGenCase is a pure Canonical XML 1.1 node-set case: canonicalize the
// node-set that Expr (evaluated over Input) selects and byte-compare to Output.
type c14nGenCase struct {
	ID     string
	Input  string
	XPath  string
	Output string
}

// sigGenCase is a signature-verification case: parse Input (a signed document)
// and verify it. Group buckets cases for the report (defCan/xpointer/dname).
type sigGenCase struct {
	ID    string
	Group string
	Input string
}

func enumerateC14NCases(fixturesRoot string) ([]c14nGenCase, error) {
	dir := filepath.Join(fixturesRoot, "c14n11")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read c14n11 dir: %w", err)
	}
	var cases []c14nGenCase
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".xpath") {
			continue
		}
		base := strings.TrimSuffix(name, ".xpath")
		cases = append(cases, c14nGenCase{
			ID:     "c14n11-" + base,
			Input:  "c14n11/" + inputForCase(base),
			XPath:  "c14n11/" + name,
			Output: "c14n11/" + base + ".output",
		})
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

// inputForCase derives the per-family input filename from a case base by
// trimming a trailing "-<digits>" (case number). xmllang-1 -> xmllang-input.xml;
// xmlbase-c14n11spec-102 -> xmlbase-c14n11spec-input.xml.
func inputForCase(base string) string {
	family := base
	if i := strings.LastIndex(base, "-"); i >= 0 {
		if isAllDigits(base[i+1:]) {
			family = base[:i]
		}
	}
	return family + "-input.xml"
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func enumerateSigCases(fixturesRoot string) ([]sigGenCase, error) {
	var cases []sigGenCase

	defCan, err := filepath.Glob(filepath.Join(fixturesRoot, "xmldsig", "defCan-*-signature.xml"))
	if err != nil {
		return nil, err
	}
	for _, p := range defCan {
		base := strings.TrimSuffix(filepath.Base(p), ".xml")
		cases = append(cases, sigGenCase{
			ID:    "sig-" + base,
			Group: "defCan",
			Input: "xmldsig/" + filepath.Base(p),
		})
	}

	xpointer, err := filepath.Glob(filepath.Join(fixturesRoot, "xmldsig", "xpointer", "xpointer-*-SUN.xml"))
	if err != nil {
		return nil, err
	}
	for _, p := range xpointer {
		base := strings.TrimSuffix(filepath.Base(p), ".xml")
		cases = append(cases, sigGenCase{
			ID:    "sig-" + base,
			Group: "xpointer",
			Input: "xmldsig/xpointer/" + filepath.Base(p),
		})
	}

	dname, err := filepath.Glob(filepath.Join(fixturesRoot, "xmldsig", "dname", "*-SUN.xml"))
	if err != nil {
		return nil, err
	}
	for _, p := range dname {
		base := strings.TrimSuffix(filepath.Base(p), ".xml")
		cases = append(cases, sigGenCase{
			ID:    "sig-" + base,
			Group: "dname",
			Input: "xmldsig/dname/" + filepath.Base(p),
		})
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

func casesSource(c14nCases []c14nGenCase, sigCases []sigGenCase) string {
	var b strings.Builder
	b.WriteString("// Code generated by w3cgen; DO NOT EDIT.\n\n")
	b.WriteString("package xmldsig_test\n\n")

	b.WriteString("var xmldsig2edC14NCases = []dsig2edC14NCase{\n")
	for _, c := range c14nCases {
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(c.ID))
		fmt.Fprintf(&b, "\t\tInput: %s,\n", strconv.Quote(c.Input))
		fmt.Fprintf(&b, "\t\tXPath: %s,\n", strconv.Quote(c.XPath))
		fmt.Fprintf(&b, "\t\tOutput: %s,\n", strconv.Quote(c.Output))
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("var xmldsig2edSigCases = []dsig2edSigCase{\n")
	for _, c := range sigCases {
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(c.ID))
		fmt.Fprintf(&b, "\t\tGroup: %s,\n", strconv.Quote(c.Group))
		fmt.Fprintf(&b, "\t\tInput: %s,\n", strconv.Quote(c.Input))
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}
