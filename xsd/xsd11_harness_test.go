package xsd_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium-w3c-tests/internal/harness"
	"github.com/lestrrat-go/helium/xsd"
)

// xstsCase is a single W3C XML Schema Test Suite (XSTS) test group, restricted
// to the XSD 1.1 subset. SchemaRel and the instance Rel paths are
// testdata/xsd11-relative slash paths; the fixture bytes live under
// testdata/xsd11 (gitignored, populated by `go run ./cmd/w3cgen fetch xsd11`).
//
// The per-contributor slices (xstsIbmCases, xstsSaxonCases, ...) are generated
// by internal/suites/xsd11 into xsd11_<contributor>_gen_test.go and each
// registers itself into xstsAllCases via an init function.
type xstsCase struct {
	ID          string
	SchemaRel   string
	SchemaValid bool
	// SchemaDocs holds every schemaDocument the schemaTest lists when there is
	// more than one; together they form a single schema. It is populated only
	// for multi-document cases (SchemaDocs[0] == SchemaRel); single-document
	// cases leave it nil and compile SchemaRel directly.
	SchemaDocs []string
	Instances  []xstsInstance
}

type xstsInstance struct {
	Name  string
	Rel   string
	Valid bool
}

// xstsAllCases is populated by the generated per-contributor files' init funcs.
var xstsAllCases []xstsCase

// slashFS adapts os.DirFS to the compiler's include resolution, which (per
// Compiler.FS) may hand the FS OS-specific separators for a local BaseDir — on
// Windows filepath-built names use backslashes that fs.ValidPath rejects. Open
// normalizes the name back to forward slashes before delegating so includes
// resolve on every OS.
type slashFS struct{ fsys fs.FS }

func (s slashFS) Open(name string) (fs.File, error) {
	return s.fsys.Open(filepath.ToSlash(name))
}

type xstsExpectations struct {
	Skip  map[string]string `json:"skip"`
	XFail map[string]string `json:"xfail"`
}

func loadXSTSExpectations(t *testing.T) xstsExpectations {
	t.Helper()
	// XSD11_EXPECTATIONS lets parallel chunks each point at their own
	// expectations copy (e.g. with one area's skips removed) without mutating
	// the shared file — so areas can be worked concurrently. Unset → default.
	p := os.Getenv("XSD11_EXPECTATIONS")
	if p == "" {
		p = filepath.Join(harness.RepoRoot(t), "expectations", "xsd11.json")
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

func TestXSD11W3C(t *testing.T) {
	exp := loadXSTSExpectations(t)
	testdataRoot := harness.SourceDir(t, "testdata/xsd11")
	for _, c := range xstsAllCases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			if reason := exp.Skip[c.ID]; reason != "" {
				t.Skip(reason)
			}
			xreason, isXFail := exp.XFail[c.ID]
			o := &caseOutcome{t: t, xfail: isXFail}
			runXSTS11Case(t, o, testdataRoot, c)
			if !isXFail {
				return
			}
			// Expected-failure case: it passes the suite when it fails the
			// conformance check (the known gap), and fails loudly when it
			// unexpectedly passes — that means helium was fixed and the entry
			// should be removed from expectations/xsd11.json.
			if len(o.fails) == 0 {
				t.Errorf("XFAIL %s unexpectedly PASSED — helium now conforms; remove it from expectations xfail (%s)", c.ID, xreason)
				return
			}
			t.Logf("xfail (%s): %s", xreason, strings.Join(o.fails, "; "))
		})
	}
}

// caseOutcome routes a case's conformance-signal assertions. For an ordinary
// case, errorf forwards to t.Errorf. For an xfail case it collects the messages
// so TestXSD11W3C can invert them (a collected failure is the expected outcome).
// Infrastructure problems (missing/unreadable fixtures) stay on t directly via
// t.Skipf/t.Fatalf and are never inverted.
type caseOutcome struct {
	t     *testing.T
	xfail bool
	fails []string
}

func (o *caseOutcome) errorf(format string, args ...any) {
	if o.xfail {
		o.fails = append(o.fails, fmt.Sprintf(format, args...))
		return
	}
	o.t.Errorf(format, args...)
}

func runXSTS11Case(t *testing.T, o *caseOutcome, testdataRoot string, c xstsCase) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			o.errorf("%s: panic: %v", c.ID, r)
		}
	}()

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
	// A schemaTest may list several schemaDocuments that together form one
	// schema (e.g. an abstract head plus substitution-group members declared in
	// other namespaces). Assemble them via an import wrapper ONLY when the
	// schema is expected to be VALID. A multi-document schema expected to be
	// INVALID exercises a per-document composition defect — e.g. "referring to
	// a namespace requires an xs:import" (src-resolve) — that is diagnosed
	// within each document's own import scope; co-assembling every document into
	// one compilation unit resolves such references across the whole schema and
	// would mask the defect, so those are compiled from the primary alone.
	if len(c.SchemaDocs) > 1 && c.SchemaValid {
		var wrapperSrc []byte
		var wrapperURI string
		var skip bool
		wrapperSrc, wrapperURI, skip = buildWrapperSchema(t, testdataRoot, c)
		if skip {
			return
		}
		doc, perr = helium.NewParser().
			BaseURI(wrapperURI).
			Parse(t.Context(), wrapperSrc)
	} else {
		doc, perr = helium.NewParser().
			BaseURI(c.SchemaRel).
			Parse(t.Context(), schemaSrc)
	}

	var schema *xsd.Schema
	var cerr error
	if perr != nil {
		cerr = perr
	} else {
		schema, cerr = xsd.NewCompiler().
			Version(xsd.Version11).
			FS(slashFS{os.DirFS(testdataRoot)}).
			BaseDir(path.Dir(c.SchemaRel)).
			Compile(t.Context(), doc)
	}

	gotSchemaValid := cerr == nil
	if gotSchemaValid != c.SchemaValid {
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
			if os.IsNotExist(err) {
				t.Skipf("%s/%s: instance fixture not fetched (%s)", c.ID, inst.Name, inst.Rel)
			}
			t.Errorf("%s/%s: read instance %s: %v", c.ID, inst.Name, inst.Rel, err)
			continue
		}
		idoc, ierr := helium.NewParser().
			BaseURI(inst.Rel).
			Parse(t.Context(), src)
		var verr error
		if ierr != nil {
			verr = ierr
		} else {
			verr = xsd.NewValidator(schema).Validate(t.Context(), idoc)
		}
		gotValid := verr == nil
		if gotValid != inst.Valid {
			o.errorf("%s/%s: instance validity: expected %t, got %t (err=%v)",
				c.ID, inst.Name, inst.Valid, gotValid, verr)
		}
	}
}

// buildWrapperSchema synthesizes an in-memory schema that xs:imports every
// document of a multi-document schemaTest. A schemaTest listing several
// schemaDocuments declares one schema assembled from all of them (e.g. an
// abstract head in one namespace with substitution-group members in others);
// compiling only the first would leave the others' components unseen. The
// wrapper is compiled under the same BaseDir/FS as SchemaRel so each imported
// document's own relative includes/imports still resolve. It returns the
// wrapper source, the base URI to parse it under, and skip=true (after calling
// t.Skip) when a referenced fixture is not present.
func buildWrapperSchema(t *testing.T, testdataRoot string, c xstsCase) ([]byte, string, bool) {
	t.Helper()
	dir := path.Dir(c.SchemaRel)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	// The wrapper carries its own targetNamespace so its no-@namespace
	// imports (for no-targetNamespace constituent documents) stay valid under
	// src-import.1.2, which forbids a no-namespace import inside a schema that
	// itself has no targetNamespace.
	b.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:x-helium-w3c:multidoc-wrapper">` + "\n")
	for _, rel := range c.SchemaDocs {
		p := filepath.Join(testdataRoot, filepath.FromSlash(rel))
		src, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch xsd11 (missing %s)", rel)
				return nil, "", true
			}
			t.Fatalf("%s: read schema %s: %v", c.ID, rel, err)
		}
		loc := rel
		if dir != "" && dir != "." {
			if r := strings.TrimPrefix(rel, dir+"/"); r != rel {
				loc = r
			}
		}
		if tns := schemaTargetNamespace(src); tns != "" {
			fmt.Fprintf(&b, "  <xs:import namespace=%q schemaLocation=%q/>\n", tns, loc)
		} else {
			fmt.Fprintf(&b, "  <xs:import schemaLocation=%q/>\n", loc)
		}
	}
	b.WriteString(`</xs:schema>` + "\n")
	return []byte(b.String()), path.Join(dir, "__helium_wrapper__.xsd"), false
}

// schemaTargetNamespace returns the targetNamespace attribute of the root
// xs:schema element, or "" when the schema has none (a no-namespace document,
// imported without a namespace attribute). Many XSTS fixtures are UTF-16 encoded
// with a BOM (encoding/xml does not auto-decode those), so the bytes are
// transcoded to UTF-8 first before scanning.
func schemaTargetNamespace(src []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(decodeXMLToUTF8(src)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, a := range se.Attr {
				if a.Name.Local == "targetNamespace" {
					return a.Value
				}
			}
			return ""
		}
	}
}

// decodeXMLToUTF8 transcodes BOM-marked UTF-16 LE/BE (and UTF-8) content to
// plain UTF-8, leaving ASCII/UTF-8 without a BOM unchanged.
func decodeXMLToUTF8(data []byte) []byte {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16BytesToUTF8(data[2:], binary.LittleEndian)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16BytesToUTF8(data[2:], binary.BigEndian)
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return data[3:]
	default:
		return data
	}
}

func utf16BytesToUTF8(b []byte, order binary.ByteOrder) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, order.Uint16(b[i:i+2]))
	}
	return []byte(string(utf16.Decode(units)))
}
