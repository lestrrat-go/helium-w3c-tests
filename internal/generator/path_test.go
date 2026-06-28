package generator_test

import (
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

func TestContainedPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	got, err := generator.ContainedPath(root, "sets/alpha.xml")
	if err != nil {
		t.Fatalf("contained path: %v", err)
	}
	want := filepath.Join(root, "sets", "alpha.xml")
	if got != want {
		t.Fatalf("contained path = %q, want %q", got, want)
	}

	tests := []string{
		"",
		"../escape.xml",
		"/etc/passwd",
		`C:\Windows\system.ini`,
		`\\server\share\catalog.xml`,
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			if _, err := generator.ContainedPath(root, tc); err == nil {
				t.Fatalf("ContainedPath(%q) succeeded, want error", tc)
			}
		})
	}
}
