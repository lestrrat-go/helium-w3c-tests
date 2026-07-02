package xsd_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xsd"
)

// The XSD 1.0 conformance suite is a manifest-driven runtime harness: rather
// than committing a ~14k-case static table (the XSD 1.0 corpus is far larger
// than the 1.1 subset), TestXSD10W3C reads the W3C XSTS catalog (suite.xml plus
// its per-contributor testSet files) at test time and runs every 1.0-applicable
// group with the DEFAULT (Version10) compiler. It reuses the same fetched corpus
// as the XSD 1.1 suite (testdata/xsd11), so no separate fetch/fixtures are
// needed, and reuses the shared helpers from xsd11_harness_test.go (slashFS,
// caseOutcome, xstsCase, xstsInstance, buildWrapperSchema, decodeXMLToUTF8,
// schemaTargetNamespace).

// XSTS catalog subset consumed by the 1.0 reader. Distinct type names from the
// generated 1.1 tables (which live in the internal/suites/xsd11 package) so the
// two suites never collide inside the xsd_test package.
type xsts10Suite struct {
	TestSetRefs []struct {
		Href string `xml:"href,attr"`
	} `xml:"testSetRef"`
}

type xsts10TestSet struct {
	Version    string        `xml:"version,attr"`
	TestGroups []xsts10Group `xml:"testGroup"`
}

type xsts10Group struct {
	Name         string            `xml:"name,attr"`
	Version      string            `xml:"version,attr"`
	SchemaTest   *xsts10SchemaTest `xml:"schemaTest"`
	InstanceTest []xsts10Instance  `xml:"instanceTest"`
}

type xsts10SchemaTest struct {
	Documents []xsts10DocRef   `xml:"schemaDocument"`
	Expected  []xsts10Expected `xml:"expected"`
}

type xsts10Instance struct {
	Name     string           `xml:"name,attr"`
	Document xsts10DocRef     `xml:"instanceDocument"`
	Expected []xsts10Expected `xml:"expected"`
}

type xsts10DocRef struct {
	Href string `xml:"href,attr"`
}

type xsts10Expected struct {
	Validity string `xml:"validity,attr"`
	Version  string `xml:"version,attr"`
}

// applies10 reports whether a test group is XSD-1.0-applicable: everything that
// is not explicitly tagged 1.1 at the group or testSet level. This mirrors the
// campaign baseline runner exactly.
func applies10(g xsts10Group, setVersion string) bool {
	if g.Version == "1.1" || setVersion == "1.1" {
		return false
	}
	return true
}

// pickValidity10 selects the version-aware expected validity, preferring an
// explicit version="1.0" result, then an unversioned one, then the first.
// Returns (valid, ok); ok is false when the chosen result is neither valid nor
// invalid. Mirrors the campaign baseline runner exactly.
func pickValidity10(exps []xsts10Expected) (bool, bool) {
	if len(exps) == 0 {
		return false, false
	}
	var chosen *xsts10Expected
	for i := range exps {
		if exps[i].Version == "1.0" {
			chosen = &exps[i]
			break
		}
	}
	if chosen == nil {
		for i := range exps {
			if exps[i].Version == "" {
				chosen = &exps[i]
				break
			}
		}
	}
	if chosen == nil {
		chosen = &exps[0]
	}
	switch chosen.Validity {
	case "valid":
		return true, true
	case "invalid":
		return false, true
	default:
		return false, false
	}
}

func resolveRel10(baseDir, href string) string { return path.Clean(path.Join(baseDir, href)) }

// buildXSD10Cases reads the XSTS catalog under testdataRoot and returns the
// XSD-1.0-applicable groups as xstsCase values. Returns ok=false when the
// catalog (suite.xml) is absent — the corpus has not been fetched.
func buildXSD10Cases(t *testing.T, testdataRoot string) ([]xstsCase, bool) {
	t.Helper()
	suiteBytes, err := os.ReadFile(filepath.Join(testdataRoot, "suite.xml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("read suite.xml: %v", err)
	}
	var suite xsts10Suite
	if err := xml.Unmarshal(suiteBytes, &suite); err != nil {
		t.Fatalf("parse suite.xml: %v", err)
	}

	var cases []xstsCase
	for _, ref := range suite.TestSetRefs {
		href := ref.Href
		if href == "" {
			continue
		}
		tsBytes, rerr := os.ReadFile(filepath.Join(testdataRoot, filepath.FromSlash(href)))
		if rerr != nil {
			// The catalog may reference testSet files not present in this
			// checkout; skip them.
			continue
		}
		var ts xsts10TestSet
		if xml.Unmarshal(tsBytes, &ts) != nil {
			continue
		}
		tsDir := path.Dir(href)
		for _, g := range ts.TestGroups {
			if !applies10(g, ts.Version) {
				continue
			}
			if g.SchemaTest == nil || len(g.SchemaTest.Documents) == 0 {
				continue
			}
			sv, ok := pickValidity10(g.SchemaTest.Expected)
			if !ok {
				continue
			}
			docs := make([]string, 0, len(g.SchemaTest.Documents))
			for _, d := range g.SchemaTest.Documents {
				docs = append(docs, resolveRel10(tsDir, d.Href))
			}
			c := xstsCase{
				ID:          href + "/" + g.Name,
				SchemaRel:   docs[0],
				SchemaValid: sv,
				SchemaDocs:  docs,
			}
			for _, it := range g.InstanceTest {
				iv, iok := pickValidity10(it.Expected)
				if !iok {
					continue
				}
				c.Instances = append(c.Instances, xstsInstance{
					Name:  it.Name,
					Rel:   resolveRel10(tsDir, it.Document.Href),
					Valid: iv,
				})
			}
			cases = append(cases, c)
		}
	}
	return cases, true
}

func loadXSD10Expectations(t *testing.T) xstsExpectations {
	t.Helper()
	p := os.Getenv("XSD10_EXPECTATIONS")
	if p == "" {
		p = filepath.Join(harness.RepoRoot(t), "expectations", "xsd10.json")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read expectations %s: %v", p, err)
	}
	var exp xstsExpectations
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("parse expectations %s: %v", p, err)
	}
	return exp
}

// xsd10Tally accumulates fine-grained pass/fail/false-accept/false-reject counts
// split by schema-test and instance-test, so the run can be sanity-checked
// against the campaign baseline (schema 14271/14399, FA 84, FR 44). false-accept
// = expected invalid but accepted; false-reject = expected valid but rejected.
type xsd10Tally struct {
	schemaPass, schemaFail, schemaFA, schemaFR      int
	instPass, instFail, instFA, instFR, instMissing int
}

func TestXSD10W3C(t *testing.T) {
	exp := loadXSD10Expectations(t)
	testdataRoot := harness.SourceDir(t, "testdata/xsd11")
	cases, ok := buildXSD10Cases(t, testdataRoot)
	if !ok {
		t.Skip("fixtures not fetched; run go run ./cmd/w3cgen fetch xsd11 (missing testdata/xsd11/suite.xml)")
	}

	var tally xsd10Tally
	for _, c := range cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			if reason := exp.Skip[c.ID]; reason != "" {
				t.Skip(reason)
			}
			xreason, isXFail := exp.XFail[c.ID]
			o := &caseOutcome{t: t, xfail: isXFail}
			runXSTS10Case(t, o, testdataRoot, c, &tally)
			if !isXFail {
				return
			}
			if len(o.fails) == 0 {
				t.Errorf("XFAIL %s unexpectedly PASSED — helium now conforms; remove it from expectations xfail (%s)", c.ID, xreason)
				return
			}
			t.Logf("xfail (%s): %s", xreason, strings.Join(o.fails, "; "))
		})
	}

	t.Logf("XSD 1.0 schema tests: pass=%d fail=%d (FA=%d FR=%d) total=%d",
		tally.schemaPass, tally.schemaFail, tally.schemaFA, tally.schemaFR,
		tally.schemaPass+tally.schemaFail)
	t.Logf("XSD 1.0 instance tests: pass=%d fail=%d (FA=%d FR=%d) missing=%d total=%d",
		tally.instPass, tally.instFail, tally.instFA, tally.instFR, tally.instMissing,
		tally.instPass+tally.instFail)
}

func runXSTS10Case(t *testing.T, o *caseOutcome, testdataRoot string, c xstsCase, tally *xsd10Tally) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			o.errorf("%s: panic: %v", c.ID, r)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	schemaPath := filepath.Join(testdataRoot, filepath.FromSlash(c.SchemaRel))
	schemaSrc, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch xsd11 (missing %s)", c.SchemaRel)
		}
		t.Fatalf("%s: read schema %s: %v", c.ID, c.SchemaRel, err)
	}

	var doc *helium.Document
	var perr error
	if len(c.SchemaDocs) > 1 && c.SchemaValid {
		wrapperSrc, wrapperURI, skip := buildWrapperSchema(t, testdataRoot, c)
		if skip {
			return
		}
		doc, perr = helium.NewParser().BaseURI(wrapperURI).Parse(ctx, wrapperSrc)
	} else {
		doc, perr = helium.NewParser().BaseURI(c.SchemaRel).Parse(ctx, schemaSrc)
	}

	var schema *xsd.Schema
	var cerr error
	if perr != nil {
		cerr = perr
	} else {
		// Default compiler: XSD 1.0. Never .Version(Version11).
		schema, cerr = xsd.NewCompiler().
			FS(slashFS{os.DirFS(testdataRoot)}).
			BaseDir(path.Dir(c.SchemaRel)).
			Compile(ctx, doc)
	}

	gotSchemaValid := cerr == nil
	if gotSchemaValid == c.SchemaValid {
		tally.schemaPass++
	} else {
		tally.schemaFail++
		if !c.SchemaValid && gotSchemaValid {
			tally.schemaFA++
		} else {
			tally.schemaFR++
		}
		o.errorf("%s: schema validity: expected %t, got %t (err=%v)",
			c.ID, c.SchemaValid, gotSchemaValid, cerr)
	}
	if !gotSchemaValid || schema == nil {
		return
	}

	for _, inst := range c.Instances {
		instPath := filepath.Join(testdataRoot, filepath.FromSlash(inst.Rel))
		src, err := os.ReadFile(instPath)
		if err != nil {
			tally.instMissing++
			continue
		}
		idoc, ierr := helium.NewParser().BaseURI(inst.Rel).Parse(ctx, src)
		var verr error
		if ierr != nil {
			verr = ierr
		} else {
			verr = xsd.NewValidator(schema).Validate(ctx, idoc)
		}
		gotValid := verr == nil
		if gotValid == inst.Valid {
			tally.instPass++
			continue
		}
		tally.instFail++
		if !inst.Valid && gotValid {
			tally.instFA++
		} else {
			tally.instFR++
		}
		o.errorf("%s/%s: instance validity: expected %t, got %t (err=%v)",
			c.ID, inst.Name, inst.Valid, gotValid, verr)
	}
}
