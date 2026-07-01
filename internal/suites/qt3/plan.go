package qt3

import (
	"path/filepath"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

// readCatalogPlan computes the manifest test counts for the QT3 suite. It reuses
// the same charset-aware catalog parsers and XPath-3.1 dependency/environment
// gating helpers the generator uses (see gen.go), so the manifest's
// runnable/skipped counts stay consistent with the emitted case tables. Base
// metadata (source presence, catalog path, pinned commit) comes from
// generator.ReadCatalogInfo.
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

	globalEnvs := make(map[string]*environment)
	for i := range cat.Environments {
		globalEnvs[cat.Environments[i].Name] = &cat.Environments[i]
	}

	info.TestSetCount = len(cat.TestSets)
	info.TestCaseCount = 0
	info.RunnableCount = 0
	info.SkippedCount = 0
	info.ExcludedCount = 0

	for _, tsRef := range cat.TestSets {
		tsPath, perr := generator.ContainedPath(sourcePath, tsRef.File)
		if perr != nil {
			continue
		}
		ts := parseTestSet(tsPath)
		info.TestCaseCount += len(ts.TestCases)

		localEnvs := make(map[string]*environment)
		for i := range ts.Environments {
			localEnvs[ts.Environments[i].Name] = &ts.Environments[i]
		}

		if !isXPathApplicable(ts.Dependencies) {
			info.ExcludedCount += len(ts.TestCases)
			continue
		}

		setSkipReason := getTestSetSkipReason(tsRef.Name)
		for _, tc := range ts.TestCases {
			mergedDeps := mergeDeps(ts.Dependencies, tc.Dependencies)
			if !isXPathApplicable(mergedDeps) || hasFeatureDependency(mergedDeps, "xpath-1.0-compatibility") {
				info.ExcludedCount++
				continue
			}

			skipReason := getSkipReason(mergedDeps)
			if skipReason == "" {
				skipReason = getTestCaseSkipReason(tsRef.Name, tc.Name)
			}
			if skipReason == "" {
				skipReason = setSkipReason
			}
			if env, _ := resolveEnvironment(tc.Environment, localEnvs, globalEnvs); env != nil {
				if envSkip := checkEnvironmentSupport(env); skipReason == "" && envSkip != "" {
					skipReason = envSkip
				}
			}

			if skipReason != "" {
				info.SkippedCount++
				continue
			}
			info.RunnableCount++
		}
	}

	return info, nil
}
