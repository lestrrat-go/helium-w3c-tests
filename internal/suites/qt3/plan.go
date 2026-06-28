package qt3

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

const qt3UnicodeVersion = "15.0"

type catalog struct {
	Environments []environment `xml:"environment"`
	TestSets     []testSetRef  `xml:"test-set"`
}

type testSetRef struct {
	Name string `xml:"name,attr"`
	File string `xml:"file,attr"`
}

type testSetFile struct {
	Name         string        `xml:"name,attr"`
	Dependencies []dependency  `xml:"dependency"`
	Environments []environment `xml:"environment"`
	TestCases    []testCase    `xml:"test-case"`
}

type environment struct {
	Name    string   `xml:"name,attr"`
	Ref     string   `xml:"ref,attr"`
	Sources []source `xml:"source"`
}

type source struct {
	Role       string `xml:"role,attr"`
	File       string `xml:"file,attr"`
	Validation string `xml:"validation,attr"`
}

type testCase struct {
	Name         string       `xml:"name,attr"`
	Environment  *environment `xml:"environment"`
	Dependencies []dependency `xml:"dependency"`
}

type dependency struct {
	Type      string `xml:"type,attr"`
	Value     string `xml:"value,attr"`
	Satisfied string `xml:"satisfied,attr"`
}

func readCatalogPlan(root string, suiteLock generator.SuiteLock) (generator.CatalogInfo, error) {
	info, err := generator.ReadCatalogInfo(root, suiteLock)
	if err != nil {
		return info, err
	}
	if !info.Present || info.CatalogPath == "" {
		return info, nil
	}

	sourcePath := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	catalogPath := filepath.Join(root, filepath.FromSlash(info.CatalogPath))
	var cat catalog
	if err := readXML(catalogPath, &cat); err != nil {
		return info, err
	}

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
		tsPath, err := generator.ContainedPath(sourcePath, tsRef.File)
		if err != nil {
			return info, fmt.Errorf("resolve QT3 test-set %q: %w", tsRef.File, err)
		}
		var ts testSetFile
		if err := readXML(tsPath, &ts); err != nil {
			return info, err
		}
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
			if env := resolveEnvironment(tc.Environment, localEnvs, globalEnvs); env != nil {
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

func readXML(path string, v any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	decoder := xml.NewDecoder(file)
	if err := decoder.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// mergeDeps combines test-set-level and test-case-level dependencies. If a
// test case has its own spec dependency, it takes precedence over the
// test-set-level spec dependency.
func mergeDeps(setDeps, caseDeps []dependency) []dependency {
	caseHasSpec := false
	for _, d := range caseDeps {
		if d.Type == "spec" {
			caseHasSpec = true
			break
		}
	}

	merged := make([]dependency, 0, len(setDeps)+len(caseDeps))
	for _, d := range setDeps {
		if d.Type == "spec" && caseHasSpec {
			continue
		}
		merged = append(merged, d)
	}
	merged = append(merged, caseDeps...)
	return merged
}

func isXPathApplicable(deps []dependency) bool {
	hasSpecDep := false
	for _, d := range deps {
		if d.Satisfied == "false" && isSupportedFeature(d) {
			return false
		}
		if d.Type != "spec" {
			continue
		}
		hasSpecDep = true
		for _, v := range strings.Fields(d.Value) {
			if !strings.HasPrefix(v, "XP") {
				continue
			}
			if d.Satisfied == "false" {
				continue
			}
			if xpVersionIncludes31(v) {
				return true
			}
		}
	}
	return !hasSpecDep
}

func isSupportedFeature(d dependency) bool {
	switch d.Type {
	case "unicode-normalization-form":
		switch d.Value {
		case "NFC", "NFD", "NFKC", "NFKD", "FULLY-NORMALIZED":
			return true
		}
	case "xsd-version":
		return d.Value == "1.1"
	}
	return false
}

func xpVersionIncludes31(v string) bool {
	if strings.HasSuffix(v, "+") {
		return true
	}
	return v == "XP31"
}

func hasFeatureDependency(deps []dependency, feature string) bool {
	for _, d := range deps {
		if d.Type == "feature" && d.Value == feature && d.Satisfied != "false" {
			return true
		}
	}
	return false
}

func getSkipReason(deps []dependency) string {
	for _, d := range deps {
		if d.Type == "feature" && d.Satisfied != "false" {
			switch d.Value {
			case "schemaImport", "schemaValidation", "schemaAware":
				return "requires XML Schema support"
			case "moduleImport":
				return "requires XQuery module import"
			case "collection-stability":
				return "requires collection stability"
			case "directory-as-collection-uri":
				return "requires directory as collection URI"
			case "fn-transform-XSLT", "fn-transform-XSLT30":
				return "requires XSLT transform"
			case "fn-load-xquery-module":
				return "requires XQuery load-xquery-module"
			case "remote_http":
				return "requires remote HTTP access"
			case "staticTyping":
				return "requires static typing"
			}
		}
		if d.Type == "spec" {
			for _, v := range strings.Fields(d.Value) {
				if v == "XP20" && d.Satisfied != "false" {
					return "requires XPath 2.0 only behavior"
				}
			}
		}
		if d.Type == "xml-version" && d.Value == "1.1" {
			return "requires XML 1.1"
		}
		if d.Type == "unicode-version" && d.Value != qt3UnicodeVersion && d.Satisfied != "false" {
			return fmt.Sprintf("requires Unicode %s", d.Value)
		}
		if d.Type == "xsd-version" && d.Value == "1.0" && d.Satisfied != "false" {
			return "requires XSD 1.0"
		}
	}
	return ""
}

func getTestCaseSkipReason(_, caseName string) string {
	switch caseName {
	case "fn-unparsed-text-012", "fn-unparsed-text-available-008",
		"fn-unparsed-text-available-010", "fn-unparsed-text-available-012",
		"fn-unparsed-text-lines-012":
		return "requires static typing"
	}
	return ""
}

func getTestSetSkipReason(string) string {
	return ""
}

func resolveEnvironment(tcEnv *environment, local, global map[string]*environment) *environment {
	if tcEnv == nil {
		return nil
	}
	if tcEnv.Ref != "" {
		if e, ok := local[tcEnv.Ref]; ok {
			return e
		}
		if e, ok := global[tcEnv.Ref]; ok {
			return e
		}
		return nil
	}
	return tcEnv
}

func checkEnvironmentSupport(env *environment) string {
	for _, src := range env.Sources {
		if src.Validation == "strict" || src.Validation == "lax" {
			return "requires schema-validated source"
		}
		if src.Role != "." && src.Role != "" && !strings.HasPrefix(src.Role, "$") {
			return fmt.Sprintf("requires source role %q", src.Role)
		}
	}
	return ""
}
