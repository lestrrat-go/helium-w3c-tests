package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
)

func WriteGoFile(path string, source string, mode GenerateMode) error {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return fmt.Errorf("format generated %s: %w", path, err)
	}
	if mode == ModeVerify {
		existing, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read generated %s: %w", path, err)
		}
		if !bytes.Equal(existing, formatted) {
			return fmt.Errorf("%s is stale; run go run ./cmd/w3cgen generate all", filepath.ToSlash(path))
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create generated directory: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write generated %s: %w", path, err)
	}
	return nil
}
