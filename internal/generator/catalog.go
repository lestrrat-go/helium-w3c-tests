package generator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type CatalogInfo struct {
	Present       bool
	SourceDir     string
	CatalogPath   string
	TestSetCount  int
	TestCaseCount int
	RunnableCount int
	SkippedCount  int
	ExcludedCount int
}

func ReadCatalogInfo(root string, suiteLock SuiteLock) (CatalogInfo, error) {
	info := CatalogInfo{
		SourceDir: suiteLock.SourceDir,
	}
	sourcePath := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	stat, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return info, nil
		}
		return info, fmt.Errorf("stat source directory %s: %w", sourcePath, err)
	}
	if !stat.IsDir() {
		return info, fmt.Errorf("source path %s is not a directory", sourcePath)
	}
	info.Present = true

	catalogPath, err := findCatalog(sourcePath)
	if err != nil {
		return info, err
	}
	if catalogPath == "" {
		return info, nil
	}
	info.CatalogPath = relativeSlash(root, catalogPath)
	if err := scanCatalog(catalogPath, &info); err != nil {
		return info, err
	}
	return info, nil
}

func findCatalog(sourcePath string) (string, error) {
	var match string
	err := filepath.WalkDir(sourcePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if name == "catalog.xml" || name == "test-suite.xml" || name == "testsuite.xml" {
			match = path
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		return match, nil
	}
	if err != nil {
		return "", fmt.Errorf("find catalog below %s: %w", sourcePath, err)
	}
	return match, nil
}

func scanCatalog(path string, info *CatalogInfo) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open catalog %s: %w", path, err)
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("scan catalog %s: %w", path, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch normalizeXMLName(start.Name.Local) {
		case "testset", "testgroup":
			info.TestSetCount++
		case "testcase":
			info.TestCaseCount++
		}
	}
}

func normalizeXMLName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}

func relativeSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
