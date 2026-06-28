package qt3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

func TestReadCatalogPlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "sources", "qt3")
	writeTestFile(t, filepath.Join(sourceDir, "catalog.xml"), `<?xml version="1.0"?>
<catalog xmlns="http://www.w3.org/2010/09/qt-fots-catalog">
  <environment name="global-schema">
    <source role="." file="doc.xml" validation="strict"/>
  </environment>
  <test-set name="alpha" file="sets/alpha.xml"/>
  <test-set name="xquery" file="sets/xquery.xml"/>
</catalog>`)
	writeTestFile(t, filepath.Join(sourceDir, "sets", "alpha.xml"), `<?xml version="1.0"?>
<test-set xmlns="http://www.w3.org/2010/09/qt-fots-catalog" name="alpha">
  <dependency type="spec" value="XP31"/>
  <test-case name="runs"/>
  <test-case name="schema">
    <dependency type="feature" value="schemaImport"/>
  </test-case>
  <test-case name="xsd10">
    <dependency type="xsd-version" value="1.0"/>
  </test-case>
  <test-case name="compat">
    <dependency type="feature" value="xpath-1.0-compatibility"/>
  </test-case>
  <test-case name="env">
    <environment ref="global-schema"/>
  </test-case>
</test-set>`)
	writeTestFile(t, filepath.Join(sourceDir, "sets", "xquery.xml"), `<?xml version="1.0"?>
<test-set xmlns="http://www.w3.org/2010/09/qt-fots-catalog" name="xquery">
  <dependency type="spec" value="XQ31"/>
  <test-case name="xquery-only"/>
</test-set>`)

	info, err := readCatalogPlan(root, generator.SuiteLock{SourceDir: "sources/qt3"})
	if err != nil {
		t.Fatalf("read catalog plan: %v", err)
	}
	if info.TestSetCount != 2 {
		t.Fatalf("TestSetCount = %d, want 2", info.TestSetCount)
	}
	if info.TestCaseCount != 6 {
		t.Fatalf("TestCaseCount = %d, want 6", info.TestCaseCount)
	}
	if info.RunnableCount != 1 {
		t.Fatalf("RunnableCount = %d, want 1", info.RunnableCount)
	}
	if info.SkippedCount != 3 {
		t.Fatalf("SkippedCount = %d, want 3", info.SkippedCount)
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
