package xsd11

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

// TestPopulateFixturesCopiesXSD10Catalog checks that the xsd11 fetch copies
// everything TestXSD10W3C reads from testdata/xsd11, not only the XSD 1.1
// fixtures: suite.xml, every testSet file it references, and the schema
// closure plus instance documents (with their schema-location hints) of the
// groups not tagged 1.1.
func TestPopulateFixturesCopiesXSD10Catalog(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "sources", "xsd11")

	writeFile(t, filepath.Join(src, "suite.xml"), `<testSuite xmlns="http://www.w3.org/XML/2004/xml-schema-test-suite/" xmlns:xlink="http://www.w3.org/1999/xlink">
  <testSetRef xlink:href="aMeta/ten.testSet"/>
  <testSetRef xlink:href="aMeta/eleven.testSet"/>
  <testSetRef xlink:href="aMeta/missing.testSet"/>
</testSuite>`)

	// A testSet with no version: its groups are XSD 1.0 cases.
	writeFile(t, filepath.Join(src, "aMeta", "ten.testSet"), `<testSet xmlns="http://www.w3.org/XML/2004/xml-schema-test-suite/" xmlns:xlink="http://www.w3.org/1999/xlink" name="ten">
  <testGroup name="g10">
    <schemaTest name="g10s">
      <schemaDocument xlink:href="../aData/ten.xsd"/>
      <expected validity="valid"/>
    </schemaTest>
    <instanceTest name="g10i">
      <instanceDocument xlink:href="../aData/ten.xml"/>
      <expected validity="valid"/>
    </instanceTest>
  </testGroup>
</testSet>`)
	writeFile(t, filepath.Join(src, "aData", "ten.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="inc/ten-inc.xsd"/>
</xs:schema>`)
	writeFile(t, filepath.Join(src, "aData", "inc", "ten-inc.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	// The validator loads instance schema-location hints, so their targets
	// (and what those include) are fixtures too.
	writeFile(t, filepath.Join(src, "aData", "ten.xml"), `<doc xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xsi:schemaLocation="http://a hint/a.xsd http://b hint/b.xsd"
  xsi:noNamespaceSchemaLocation="hint/nn.xsd"/>`)
	writeFile(t, filepath.Join(src, "aData", "hint", "a.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="a-inc.xsd"/>
</xs:schema>`)
	writeFile(t, filepath.Join(src, "aData", "hint", "a-inc.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	writeFile(t, filepath.Join(src, "aData", "hint", "b.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	writeFile(t, filepath.Join(src, "aData", "hint", "nn.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)

	// A 1.1 testSet: its group is an XSD 1.1 case.
	writeFile(t, filepath.Join(src, "aMeta", "eleven.testSet"), `<testSet xmlns="http://www.w3.org/XML/2004/xml-schema-test-suite/" xmlns:xlink="http://www.w3.org/1999/xlink" name="eleven" version="1.1">
  <testGroup name="g11">
    <schemaTest name="g11s">
      <schemaDocument xlink:href="../aData/eleven.xsd"/>
      <expected validity="valid"/>
    </schemaTest>
  </testGroup>
</testSet>`)
	writeFile(t, filepath.Join(src, "aData", "eleven.xsd"), `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)

	// Not referenced by any case: must not be copied.
	writeFile(t, filepath.Join(src, "README.md"), `readme`)

	err := Suite{}.populateFixtures(root, generator.SuiteLock{SourceDir: "sources/xsd11"})
	if err != nil {
		t.Fatalf("populateFixtures: %v", err)
	}

	dest := filepath.Join(root, "testdata", "xsd11")
	want := []string{
		"suite.xml",
		"aMeta/ten.testSet",
		"aMeta/eleven.testSet",
		"aData/ten.xsd",
		"aData/inc/ten-inc.xsd",
		"aData/ten.xml",
		"aData/hint/a.xsd",
		"aData/hint/a-inc.xsd",
		"aData/hint/b.xsd",
		"aData/hint/nn.xsd",
		"aData/eleven.xsd",
	}
	for _, rel := range want {
		if _, serr := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); serr != nil {
			t.Errorf("fixture %s not copied: %v", rel, serr)
		}
	}
	if _, serr := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(serr) {
		t.Errorf("unreferenced README.md was copied (stat err = %v)", serr)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
