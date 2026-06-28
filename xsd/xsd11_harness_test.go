package xsd_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"

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
	Instances   []xstsInstance
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
	p := filepath.Join(harness.RepoRoot(t), "expectations", "xsd11.json")
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
			runXSTS11Case(t, testdataRoot, c)
		})
	}
}

func runXSTS11Case(t *testing.T, testdataRoot string, c xstsCase) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: panic: %v", c.ID, r)
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

	doc, perr := helium.NewParser().Parse(t.Context(), schemaSrc)

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
		t.Errorf("%s: schema validity: expected %t, got %t (err=%v)",
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
		idoc, ierr := helium.NewParser().Parse(t.Context(), src)
		var verr error
		if ierr != nil {
			verr = ierr
		} else {
			verr = xsd.NewValidator(schema).Validate(t.Context(), idoc)
		}
		gotValid := verr == nil
		if gotValid != inst.Valid {
			t.Errorf("%s/%s: instance validity: expected %t, got %t (err=%v)",
				c.ID, inst.Name, inst.Valid, gotValid, verr)
		}
	}
}
