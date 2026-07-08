package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type LockFile struct {
	Suites map[string]SuiteLock `json:"suites"`
}

type SuiteLock struct {
	Type      string `json:"type,omitempty"`
	Repo      string `json:"repo,omitempty"`
	URL       string `json:"url,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Sha256    string `json:"sha256,omitempty"`
	SourceDir string `json:"sourceDir"`
}

func AbsRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root %s: %w", root, err)
	}
	return abs, nil
}

func LoadLock(root string) (LockFile, error) {
	path := filepath.Join(root, "suites.lock.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return LockFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return LockFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if lock.Suites == nil {
		lock.Suites = make(map[string]SuiteLock)
	}
	return lock, nil
}

func (l LockFile) Suite(name string) (SuiteLock, bool) {
	suiteLock, ok := l.Suites[name]
	return suiteLock, ok
}

func (l SuiteLock) SourceKind() string {
	if l.Type != "" {
		return l.Type
	}
	if l.Repo != "" {
		return "git"
	}
	return "manual"
}
