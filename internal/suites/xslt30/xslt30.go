package xslt30

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
	return "xslt30"
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
		PackageName:  "xslt3_test",
		TestName:     "TestXSLT30W3CManifestAvailable",
		DisplayName:  "XSLT 3.0",
		SuiteName:    s.Name(),
		FetchCommand: "go run ./cmd/w3cgen fetch xslt30",
		Lock:         suiteLock,
		Catalog:      catalog,
	})
	return generator.WriteGoFile(filepath.Join(genCtx.Root, "xslt3", "w3c_manifest_gen_test.go"), source, mode)
}
