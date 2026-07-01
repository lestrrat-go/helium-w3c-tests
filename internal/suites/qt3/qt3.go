package qt3

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
	return "qt3"
}

// Fetch clones the upstream QT3 suite, then copies the context-document and
// resource fixtures its catalog references from sources/qt3 into testdata/qt3ts,
// preserving the suite-root-relative layout so document()/fn:doc/fn:json-doc and
// collection lookups resolve at test time.
func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	if err := generator.FetchSource(ctx, genCtx.Root, s.Name(), suiteLock); err != nil {
		return err
	}
	return s.populateFixtures(genCtx.Root, suiteLock)
}

func (s Suite) populateFixtures(root string, suiteLock generator.SuiteLock) (err error) {
	defer recoverGenErr(&err)
	sourceRoot := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	destRoot := filepath.Join(root, "testdata", "qt3ts")

	// Start from a clean tree so a stale file from a previous fetch can never
	// mask a missing-copy bug.
	if err := os.RemoveAll(destRoot); err != nil {
		return err
	}

	_, docFiles, resourceFiles := collectTests(sourceRoot)
	n, err := copyDocs(sourceRoot, destRoot, docFiles, resourceFiles)
	if err != nil {
		return err
	}

	// Overlay the committed curated fixtures (hand-edited or manually-added
	// files a static catalog scan cannot reproduce) on top of the source copy.
	overlayDir := filepath.Join(root, "fixtures", "qt3ts")
	m, err := copyOverlay(overlayDir, destRoot)
	if err != nil {
		return err
	}
	fmt.Printf("qt3: copied %d fixtures + %d curated overlay files into testdata/qt3ts\n", n, m)
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
		PackageName:  "xpath3_test",
		TestName:     "TestQT3W3CManifestAvailable",
		DisplayName:  "QT3",
		SuiteName:    s.Name(),
		FetchCommand: "go run ./cmd/w3cgen fetch qt3",
		Lock:         suiteLock,
		Catalog:      catalog,
	})
	if werr := generator.WriteGoFile(filepath.Join(genCtx.Root, "xpath3", "qt3_manifest_gen_test.go"), manifest, mode); werr != nil {
		return werr
	}

	// The per-category case tables can only be generated once the suite source
	// has been fetched; when it is absent, emit just the manifest.
	sourceRoot := filepath.Join(genCtx.Root, filepath.FromSlash(suiteLock.SourceDir))
	if _, statErr := os.Stat(filepath.Join(sourceRoot, "catalog.xml")); statErr != nil {
		return nil
	}
	tests, _, _ := collectTests(sourceRoot)
	return writeCategoryFiles(filepath.Join(genCtx.Root, "xpath3"), tests, mode)
}

// copyOverlay copies the committed curated-fixture overlay from overlayDir into
// destDir, overriding any file the source copy already produced.
func copyOverlay(overlayDir, destDir string) (int, error) {
	if _, err := os.Stat(overlayDir); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	copied := 0
	err := filepath.WalkDir(overlayDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(overlayDir, path)
		if err != nil {
			return err
		}
		dst, err := generator.ContainedPath(destDir, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		if err := copyAsset(path, dst); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}
