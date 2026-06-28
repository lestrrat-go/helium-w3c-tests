package xslt30

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

type catalog struct {
	TestSets []testSetRef `xml:"test-set"`
}

type testSetRef struct {
	Name string `xml:"name,attr"`
	File string `xml:"file,attr"`
}

type testSetFile struct {
	Name         string        `xml:"name,attr"`
	Dependencies *dependencies `xml:"dependencies"`
	TestCases    []testCase    `xml:"test-case"`
}

type dependencies struct {
	Children []dependency `xml:",any"`
}

type dependency struct {
	XMLName   xml.Name `xml:""`
	Value     string   `xml:"value,attr"`
	Satisfied string   `xml:"satisfied,attr"`
}

type testCase struct {
	Name         string        `xml:"name,attr"`
	Dependencies *dependencies `xml:"dependencies"`
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

	info.TestSetCount = len(cat.TestSets)
	info.TestCaseCount = 0
	info.RunnableCount = 0
	info.SkippedCount = 0
	info.ExcludedCount = 0

	streamingUnlocked := 0
	for _, tsRef := range cat.TestSets {
		tsPath, err := generator.ContainedPath(sourcePath, tsRef.File)
		if err != nil {
			return info, fmt.Errorf("resolve XSLT 3.0 test-set %q: %w", tsRef.File, err)
		}
		var ts testSetFile
		if err := readXML(tsPath, &ts); err != nil {
			return info, err
		}
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

			skipReason := setSkip
			if skipReason == "" {
				skipReason = getCaseSkipReason(ts.Dependencies, tc.Dependencies)
			}
			if skipReason == "" && category == "strm" {
				streamingUnlocked++
				if strmUnlockLimit > 0 && streamingUnlocked > strmUnlockLimit {
					skipReason = "streaming test suite disabled"
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

func getSetSkipReason(name string, deps *dependencies) string {
	switch name {
	case "load-xquery-module":
		return "requires XQuery load-xquery-module"
	}
	if deps != nil {
		if reason := getDepsSkipReason(deps); reason != "" {
			return reason
		}
	}
	return ""
}

const strmUnlockLimit = 2542

func getCategorySkipReason(string) string {
	return ""
}

func getCaseSkipReason(setDeps *dependencies, caseDeps *dependencies) string {
	if setDeps != nil {
		if reason := getDepsSkipReason(setDeps); reason != "" {
			return reason
		}
	}
	if caseDeps != nil {
		if reason := getDepsSkipReason(caseDeps); reason != "" {
			return reason
		}
	}
	return ""
}

func getDepsSkipReason(deps *dependencies) string {
	for _, d := range deps.Children {
		switch d.XMLName.Local {
		case "spec":
			if !specSupported(d.Value) {
				return fmt.Sprintf("unsupported spec: %s", d.Value)
			}
		case "feature":
			if d.Satisfied == "false" {
				if featureSupported(d.Value) {
					return fmt.Sprintf("feature present but test requires absent: %s", d.Value)
				}
				continue
			}
			if !featureSupported(d.Value) {
				return fmt.Sprintf("unsupported feature: %s", d.Value)
			}
		case "year_component_values":
			if d.Satisfied == "false" {
				if yearComponentValueSupported(d.Value) {
					return fmt.Sprintf("year component value present but test requires absent: %s", d.Value)
				}
				continue
			}
			if !yearComponentValueSupported(d.Value) {
				return fmt.Sprintf("unsupported year component value: %s", d.Value)
			}
		case "on-multiple-match", "package_version_resolution":
		case "enable_assertions":
			if d.Satisfied == "false" {
				return "test requires assertions disabled; we evaluate assertions"
			}
		case "ignore_doc_failure":
			if d.Satisfied == "true" {
				return "processor raises FODC0002 instead of ignoring document() failures"
			}
		}
	}
	return ""
}

func specSupported(spec string) bool {
	for _, s := range strings.Fields(spec) {
		switch s {
		case "XSLT10+", "XSLT20+", "XSLT30", "XSLT30+":
			return true
		}
	}
	return false
}

func isExcludedTestCase(name string) bool {
	return strings.HasPrefix(name, "unicode90-")
}

func featureSupported(feature string) bool {
	switch feature {
	case "backwards_compatibility", "Saxon-PE", "Saxon-EE":
		return false
	}
	return true
}

func yearComponentValueSupported(value string) bool {
	switch value {
	case "support negative year", "support year above 9999", "support year zero":
		return true
	}
	return false
}

func categoryFromCatalogPath(file string) string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	if len(parts) >= 2 && parts[0] == "tests" {
		return parts[1]
	}
	return ""
}
