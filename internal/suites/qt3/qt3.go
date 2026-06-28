package qt3

import (
	"context"
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

func (s Suite) Fetch(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock) error {
	return generator.FetchSource(ctx, genCtx.Root, s.Name(), suiteLock)
}

func (s Suite) Generate(ctx context.Context, genCtx generator.Context, suiteLock generator.SuiteLock, mode generator.GenerateMode) error {
	_ = ctx
	catalog, err := readCatalogPlan(genCtx.Root, suiteLock)
	if err != nil {
		return err
	}
	source := generator.ManifestTestSource(generator.ManifestTest{
		PackageName:  "xpath3_test",
		TestName:     "TestQT3W3CManifestAvailable",
		DisplayName:  "QT3",
		SuiteName:    s.Name(),
		FetchCommand: "go run ./cmd/w3cgen fetch qt3",
		Lock:         suiteLock,
		Catalog:      catalog,
	})
	return generator.WriteGoFile(filepath.Join(genCtx.Root, "xpath3", "qt3_manifest_gen_test.go"), source, mode)
}
