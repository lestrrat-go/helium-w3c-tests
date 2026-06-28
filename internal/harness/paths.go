package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func RepoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine harness source path")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find repository root")
		}
		dir = parent
	}
}

func SourceDir(t testing.TB, rel string) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), filepath.FromSlash(rel))
}
