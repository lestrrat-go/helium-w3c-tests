package xslt30

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

type Suite struct{}

func New() Suite {
	return Suite{}
}

func (Suite) Name() string {
	return "xslt30"
}

// Fetch clones the upstream XSLT 3.0 suite, then copies every fixture its
// catalog references (stylesheets and their transitive xsl:include/import
// closure, source documents, packages, schemas, collections) from
// sources/xslt30 into testdata/xslt30, preserving the suite-root-relative
// layout so those references resolve at test time.
func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	if err := generator.FetchSource(ctx, genCtx.Root, s.Name(), suiteLock); err != nil {
		return err
	}
	return s.populateFixtures(genCtx.Root, suiteLock)
}

func (s Suite) populateFixtures(root string, suiteLock generator.SuiteLock) (err error) {
	defer recoverGenErr(&err)
	sourceRoot := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	destRoot := filepath.Join(root, "testdata", "xslt30")

	// Start from a clean tree so a stale file from a previous fetch can never
	// mask a missing-copy bug or keep the "a missing needed fixture fails
	// loudly at test time" property honest.
	if err := os.RemoveAll(destRoot); err != nil {
		return err
	}

	_, assetFiles := collectTests(sourceRoot)
	n, err := copyAssets(sourceRoot, destRoot, assetFiles)
	if err != nil {
		return err
	}

	// Overlay the committed curated fixtures (runtime-only docs the catalog scan
	// misses, plus hand-edited fixtures) on top of the reproducible source copy.
	overlayDir := filepath.Join(root, "fixtures", "xslt30")
	m, err := copyOverlay(overlayDir, destRoot)
	if err != nil {
		return err
	}
	fmt.Printf("xslt30: copied %d fixtures + %d curated overlay files into testdata/xslt30\n", n, m)
	return nil
}

func (s Suite) Generate(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock, mode generator.GenerateMode) (err error) {
	_ = ctx
	defer recoverGenErr(&err)

	catalog, cerr := readCatalogPlan(genCtx.Root, suiteLock)
	if cerr != nil {
		return cerr
	}
	manifest := generator.ManifestTestSource(generator.ManifestTest{
		PackageName:  "xslt3_test",
		TestName:     "TestXSLT30W3CManifestAvailable",
		DisplayName:  "XSLT 3.0",
		SuiteName:    s.Name(),
		FetchCommand: "go run ./cmd/w3cgen fetch xslt30",
		Lock:         suiteLock,
		Catalog:      catalog,
	})
	if werr := generator.WriteGoFile(filepath.Join(genCtx.Root, "xslt3", "w3c_manifest_gen_test.go"), manifest, mode); werr != nil {
		return werr
	}

	// The per-category case tables can only be generated once the suite source
	// has been fetched. When it is absent, emit just the manifest (which skips
	// at run time) so `w3cgen generate` still succeeds on a fresh checkout.
	sourceRoot := filepath.Join(genCtx.Root, filepath.FromSlash(suiteLock.SourceDir))
	if _, statErr := os.Stat(filepath.Join(sourceRoot, "catalog.xml")); statErr != nil {
		return nil
	}
	tests, _ := collectTests(sourceRoot)
	return writeCategoryFiles(filepath.Join(genCtx.Root, "xslt3"), tests, mode)
}
