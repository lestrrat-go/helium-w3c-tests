package generator

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ContainedPath joins root and relPath, rejecting absolute paths and paths
// that escape root. Catalog file references are upstream-controlled data, so
// every generator resolves them through this helper before reading fixtures.
func ContainedPath(root, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("empty path")
	}
	if IsAbsoluteAnyOS(relPath) {
		return "", fmt.Errorf("absolute path not allowed: %q", relPath)
	}
	cleaned := filepath.Clean(filepath.Join(root, filepath.FromSlash(relPath)))
	rel, err := filepath.Rel(root, cleaned)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root %q: %q", root, relPath)
	}
	return cleaned, nil
}

// IsAbsoluteAnyOS reports whether p is absolute under POSIX or Windows path
// conventions, independent of the host OS.
func IsAbsoluteAnyOS(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	if len(p) >= 3 && isASCIIAlpha(p[0]) && p[1] == ':' {
		return p[2] == '/' || p[2] == '\\'
	}
	return false
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
