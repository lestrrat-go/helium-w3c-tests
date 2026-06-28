package xslt30

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

func TestReadCatalogPlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "sources", "xslt30")
	writeTestFile(t, filepath.Join(sourceDir, "catalog.xml"), `<?xml version="1.0"?>
<catalog>
  <test-set name="basic" file="tests/basic/_basic-test-set.xml"/>
  <test-set name="load-xquery-module" file="tests/load/_load-test-set.xml"/>
  <test-set name="catalog" file="tests/catalog/_catalog-test-set.xml"/>
</catalog>`)
	writeTestFile(t, filepath.Join(sourceDir, "tests", "basic", "_basic-test-set.xml"), `<?xml version="1.0"?>
<test-set name="basic">
  <dependencies>
    <spec value="XSLT30"/>
  </dependencies>
  <test-case name="runs"/>
  <test-case name="compat">
    <dependencies>
      <feature value="backwards_compatibility"/>
    </dependencies>
  </test-case>
  <test-case name="absent-streaming">
    <dependencies>
      <feature value="streaming" satisfied="false"/>
    </dependencies>
  </test-case>
  <test-case name="negative-year-absent">
    <dependencies>
      <year_component_values value="support negative year" satisfied="false"/>
    </dependencies>
  </test-case>
  <test-case name="unicode90-a"/>
</test-set>`)
	writeTestFile(t, filepath.Join(sourceDir, "tests", "load", "_load-test-set.xml"), `<?xml version="1.0"?>
<test-set name="load-xquery-module">
  <test-case name="load-xquery-module-001"/>
</test-set>`)
	writeTestFile(t, filepath.Join(sourceDir, "tests", "catalog", "_catalog-test-set.xml"), `<?xml version="1.0"?>
<test-set name="catalog">
  <test-case name="catalog-001"/>
</test-set>`)

	info, err := readCatalogPlan(root, generator.SuiteLock{SourceDir: "sources/xslt30"})
	if err != nil {
		t.Fatalf("read catalog plan: %v", err)
	}
	if info.TestSetCount != 3 {
		t.Fatalf("TestSetCount = %d, want 3", info.TestSetCount)
	}
	if info.TestCaseCount != 7 {
		t.Fatalf("TestCaseCount = %d, want 7", info.TestCaseCount)
	}
	if info.RunnableCount != 1 {
		t.Fatalf("RunnableCount = %d, want 1", info.RunnableCount)
	}
	if info.SkippedCount != 4 {
		t.Fatalf("SkippedCount = %d, want 4", info.SkippedCount)
	}
	if info.ExcludedCount != 2 {
		t.Fatalf("ExcludedCount = %d, want 2", info.ExcludedCount)
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
