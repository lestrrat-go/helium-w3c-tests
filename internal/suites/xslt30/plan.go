package xslt30

import (
	"path/filepath"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

// readCatalogPlan computes the manifest test counts for the XSLT 3.0 suite. It
// reuses the same charset-aware catalog parsers and dependency-gating helpers
// the generator uses (see gen.go), so the manifest's runnable/skipped counts
// stay consistent with the emitted case tables. Base metadata (source presence,
// catalog path, pinned commit) comes from generator.ReadCatalogInfo.
func readCatalogPlan(root string, suiteLock generator.SuiteLock) (info generator.CatalogInfo, err error) {
	defer recoverGenErr(&err)

	info, err = generator.ReadCatalogInfo(root, suiteLock)
	if err != nil {
		return info, err
	}
	if !info.Present || info.CatalogPath == "" {
		return info, nil
	}

	sourcePath := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	cat := parseCatalog(filepath.Join(root, filepath.FromSlash(info.CatalogPath)))

	info.TestSetCount = len(cat.TestSets)
	info.TestCaseCount = 0
	info.RunnableCount = 0
	info.SkippedCount = 0
	info.ExcludedCount = 0

	streamingUnlocked := 0
	for _, tsRef := range cat.TestSets {
		tsRel, ok := catalogRelPath(sourcePath, ".", tsRef.File)
		if !ok {
			continue
		}
		ts := parseTestSet(filepath.Join(sourcePath, tsRel))
		info.TestCaseCount += len(ts.TestCases)

		if tsRef.Name == "catalog" {
			info.ExcludedCount += len(ts.TestCases)
			continue
		}

		category := categoryFromCatalogPath(tsRef.File)
		setSkip := getCategorySkipReason(category)
		if setSkip == "" {
			setSkip = getSetSkipReason(tsRef.Name, ts.Dependencies)
		}

		for _, tc := range ts.TestCases {
			if isExcludedTestCase(tc.Name) {
				info.ExcludedCount++
				continue
			}
			skip := setSkip
			if skip == "" {
				skip = getCaseSkipReason(ts.Dependencies, tc.Dependencies)
			}
			if skip == "" && category == "strm" {
				streamingUnlocked++
				if strmUnlockLimit > 0 && streamingUnlocked > strmUnlockLimit {
					skip = "streaming test suite disabled"
				}
			}
			if skip != "" {
				info.SkippedCount++
				continue
			}
			info.RunnableCount++
		}
	}
	return info, nil
}
