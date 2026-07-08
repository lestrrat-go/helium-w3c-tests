package generator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

func FetchSource(ctx context.Context, root string, suiteName string, suiteLock SuiteLock) error {
	switch suiteLock.SourceKind() {
	case "git":
		return fetchGit(ctx, root, suiteLock)
	case "zip":
		return fetchZip(ctx, root, suiteName, suiteLock)
	case "manual":
		return fmt.Errorf("%s source is manual; download %s into %s", suiteName, suiteLock.URL, suiteLock.SourceDir)
	default:
		return fmt.Errorf("%s has unsupported source type %q", suiteName, suiteLock.SourceKind())
	}
}

// fetchZip downloads the suite's pinned zip archive, verifies it against the
// locked sha256, and extracts it into sourceDir. Every archive entry is resolved
// through ContainedPath so a malicious archive cannot escape sourceDir (zip-slip).
// When the lock has no sha256 yet, the computed digest is printed so it can be
// pinned, and extraction proceeds.
func fetchZip(ctx context.Context, root string, suiteName string, suiteLock SuiteLock) error {
	if suiteLock.URL == "" {
		return fmt.Errorf("%s zip source has no url", suiteName)
	}

	sourcePath := filepath.Join(root, filepath.FromSlash(suiteLock.SourceDir))
	if _, err := os.Stat(sourcePath); err == nil {
		fmt.Printf("%s: %s already present; skipping download\n", suiteName, suiteLock.SourceDir)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", sourcePath, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, suiteLock.URL, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", suiteLock.URL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", suiteLock.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %s", suiteLock.URL, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", suiteLock.URL, err)
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if suiteLock.Sha256 == "" {
		fmt.Printf("%s: downloaded %d bytes, sha256=%s (pin this in suites.lock.json)\n", suiteName, len(data), digest)
	} else if digest != suiteLock.Sha256 {
		return fmt.Errorf("%s zip sha256 mismatch: got %s, want %s", suiteName, digest, suiteLock.Sha256)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open %s zip: %w", suiteName, err)
	}
	for _, entry := range reader.File {
		if err := extractZipEntry(sourcePath, entry); err != nil {
			return err
		}
	}
	fmt.Printf("%s: extracted %d entries into %s\n", suiteName, len(reader.File), suiteLock.SourceDir)
	return nil
}

func extractZipEntry(destRoot string, entry *zip.File) error {
	dst, err := ContainedPath(destRoot, entry.Name)
	if err != nil {
		return fmt.Errorf("resolve zip entry %q: %w", entry.Name, err)
	}
	if entry.FileInfo().IsDir() {
		return os.MkdirAll(dst, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", dst, err)
	}
	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", entry.Name, err)
	}
	defer rc.Close()
	out, err := os.Create(dst) //nolint:gosec // dst is contained under destRoot
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec // fixtures are trusted W3C data
		out.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
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
