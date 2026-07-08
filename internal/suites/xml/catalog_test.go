package xml

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadCases exercises the two catalog shapes the suite must handle: a rooted
// <TESTCASES> collection and a bare fragment of sibling <TEST> elements (the sun
// shape), both pulled in through the top catalog's external-entity + xml:base
// wiring, plus nested TESTCASES and OUTPUT/closure resolution.
func TestReadCases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	write(t, filepath.Join(root, "xmlconf.xml"), `<?xml version="1.0"?>
<!DOCTYPE TESTSUITE SYSTEM "testcases.dtd" [
  <!ENTITY jclark SYSTEM "xmltest/xmltest.xml">
  <!ENTITY sun-valid SYSTEM "sun/sun-valid.xml">
]>
<TESTSUITE>
<TESTCASES xml:base="xmltest/">
  &jclark;
</TESTCASES>
<TESTCASES xml:base="sun/">
  &sun-valid;
</TESTCASES>
</TESTSUITE>`)

	// Rooted collection with a nested TESTCASES and an OUTPUT reference.
	write(t, filepath.Join(root, "xmltest", "xmltest.xml"), `<?xml version="1.0"?>
<TESTCASES PROFILE="rooted">
  <TEST TYPE="not-wf" ENTITIES="none" ID="nw-1" URI="not-wf/001.xml" SECTIONS="3.1"/>
  <TESTCASES>
    <TEST TYPE="valid" ID="v-1" URI="valid/001.xml" OUTPUT="valid/out/001.xml"/>
  </TESTCASES>
</TESTCASES>`)

	// Bare fragment: sibling TEST elements, no wrapping root (the sun shape).
	write(t, filepath.Join(root, "sun", "sun-valid.xml"), `<?xml version="1.0"?>
<!-- a fragment -->
<TEST URI="valid/pe01.xml" ID="pe01" ENTITIES="parameter" TYPE="valid"/>
<TEST URI="valid/dtd00.xml" ID="dtd00" TYPE="valid" OUTPUT="valid/out/dtd00.xml"/>`)

	// A couple of referenced fixtures so the closure walk finds them.
	write(t, filepath.Join(root, "xmltest", "not-wf", "001.xml"), `<doc/>`)
	write(t, filepath.Join(root, "xmltest", "valid", "001.xml"), `<doc/>`)

	byCollection, collections, total, err := readCases(root)
	if err != nil {
		t.Fatalf("readCases: %v", err)
	}
	if total != 4 {
		t.Fatalf("total cases = %d, want 4", total)
	}
	want := map[string]int{"xmltest": 2, "sun": 2}
	for _, c := range collections {
		if len(byCollection[c]) != want[c] {
			t.Errorf("collection %s: %d cases, want %d", c, len(byCollection[c]), want[c])
		}
	}

	byID := map[string]genCase{}
	for _, cases := range byCollection {
		for _, gc := range cases {
			byID[gc.ID] = gc
		}
	}

	// xml:base chains onto the URI, and the nested TESTCASES inherits its parent
	// base.
	if got := byID["nw-1"].URI; got != "xmltest/not-wf/001.xml" {
		t.Errorf("nw-1 URI = %q, want xmltest/not-wf/001.xml", got)
	}
	if got := byID["v-1"].URI; got != "xmltest/valid/001.xml" {
		t.Errorf("v-1 URI (nested) = %q, want xmltest/valid/001.xml", got)
	}
	if got := byID["v-1"].Output; got != "xmltest/valid/out/001.xml" {
		t.Errorf("v-1 Output = %q, want xmltest/valid/out/001.xml", got)
	}
	if got := byID["pe01"].URI; got != "sun/valid/pe01.xml" {
		t.Errorf("pe01 URI (bare fragment) = %q, want sun/valid/pe01.xml", got)
	}
	if got := byID["pe01"].Entities; got != "parameter" {
		t.Errorf("pe01 Entities = %q, want parameter", got)
	}
}

// TestReadCasesCorruptCatalog checks that a corrupt/tampered source — where a
// referenced entity is undeclared, or a referenced collection catalog is missing
// — is a hard error rather than a silently smaller generated suite.
func TestReadCasesCorruptCatalog(t *testing.T) {
	t.Parallel()

	t.Run("undeclared entity reference", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		// jclark is declared; ghost is referenced but never declared.
		write(t, filepath.Join(root, "xmlconf.xml"), `<?xml version="1.0"?>
<!DOCTYPE TESTSUITE SYSTEM "testcases.dtd" [
  <!ENTITY jclark SYSTEM "xmltest/xmltest.xml">
]>
<TESTSUITE>
<TESTCASES xml:base="xmltest/">&jclark;</TESTCASES>
<TESTCASES xml:base="ghost/">&ghost;</TESTCASES>
</TESTSUITE>`)
		write(t, filepath.Join(root, "xmltest", "xmltest.xml"),
			`<TESTCASES><TEST TYPE="valid" ID="v-1" URI="001.xml"/></TESTCASES>`)
		write(t, filepath.Join(root, "xmltest", "001.xml"), `<doc/>`)

		if _, _, _, err := readCases(root); err == nil {
			t.Fatal("expected an error for an undeclared entity reference, got nil")
		}
	})

	t.Run("missing collection catalog", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, filepath.Join(root, "xmlconf.xml"), `<?xml version="1.0"?>
<!DOCTYPE TESTSUITE SYSTEM "testcases.dtd" [
  <!ENTITY jclark SYSTEM "xmltest/xmltest.xml">
]>
<TESTSUITE>
<TESTCASES xml:base="xmltest/">&jclark;</TESTCASES>
</TESTSUITE>`)
		// The declared xmltest/xmltest.xml is deliberately absent on disk.
		if _, _, _, err := readCases(root); err == nil {
			t.Fatal("expected an error for a missing referenced catalog, got nil")
		}
	})
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
