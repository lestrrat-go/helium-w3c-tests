package generator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func FetchSource(ctx context.Context, root string, suiteName string, suiteLock SuiteLock) error {
	switch suiteLock.SourceKind() {
	case "git":
		return fetchGit(ctx, root, suiteLock)
	case "manual":
		return fmt.Errorf("%s source is manual; download %s into %s", suiteName, suiteLock.URL, suiteLock.SourceDir)
	default:
		return fmt.Errorf("%s has unsupported source type %q", suiteName, suiteLock.SourceKind())
	}
}

func fetchGit(ctx context.Context, root string, suiteLock SuiteLock) error {
	if suiteLock.Repo == "" {
		return fmt.Errorf("git source has no repo")
	}
	if suiteLock.Commit == "" {
		return fmt.Errorf("git source %s has no pinned commit", suiteLock.Repo)
	}

	sourcePath := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	if _, err := os.Stat(sourcePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", sourcePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
			return fmt.Errorf("create source parent: %w", err)
		}
		if err := runGit(ctx, root, "clone", "--no-checkout", suiteLock.Repo, sourcePath); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Join(sourcePath, ".git")); err != nil {
		return fmt.Errorf("%s exists but is not a git checkout", sourcePath)
	}

	if err := runGit(ctx, sourcePath, "fetch", "--tags", "origin"); err != nil {
		return err
	}
	return runGit(ctx, sourcePath, "checkout", "--detach", suiteLock.Commit)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}
