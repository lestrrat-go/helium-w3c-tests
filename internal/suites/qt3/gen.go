// This file parses the W3C QT3 (XPath/XQuery Test Suite 3) catalog and builds
// the table-driven test cases the suite emits into xpath3/qt3_<category>_gen_test.go,
// plus the set of context-document/resource fixtures Fetch copies into
// testdata/qt3ts. It is driven by Suite.Fetch and Suite.Generate (see qt3.go).
package qt3

import (
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
	"golang.org/x/net/html/charset"
)

// ──────────────────────────────────────────────────────────────────────
// QT3 catalog XML types
// ──────────────────────────────────────────────────────────────────────

const qt3NS = "http://www.w3.org/2010/09/qt-fots-catalog"
const qt3UnicodeVersion = "15.0"

type catalog struct {
	Environments []environment `xml:"environment"`
	TestSets     []testSetRef  `xml:"test-set"`
}

type testSetRef struct {
	Name string `xml:"name,attr"`
	File string `xml:"file,attr"`
}

type testSetFile struct {
	Name         string        `xml:"name,attr"`
	Dependencies []dependency  `xml:"dependency"`
	Environments []environment `xml:"environment"`
	TestCases    []testCase    `xml:"test-case"`
}

type staticBaseURI struct {
	URI string `xml:"uri,attr"`
}

type environment struct {
	Name          string          `xml:"name,attr"`
	Ref           string          `xml:"ref,attr"`
	Sources       []source        `xml:"source"`
	Schemas       []schema        `xml:"schema"`
	Collections   []collection    `xml:"collection"`
	Resources     []resource      `xml:"resource"`
	Namespaces    []namespace     `xml:"namespace"`
	Collations    []collation     `xml:"collation"`
	DecimalFormat []decimalFormat `xml:"decimal-format"`
	Params        []param         `xml:"param"`
	StaticBaseURI *staticBaseURI  `xml:"static-base-uri"`
}

// schema is a catalog <schema uri=".." file=".."/> element: a schema whose
// components are in scope for the environment. uri is the schema's target
// namespace (empty for a no-namespace schema); file is the .xsd path relative
// to the test-set (or, for a global environment, the suite root).
type schema struct {
	URI  string `xml:"uri,attr"`
	File string `xml:"file,attr"`
	Role string `xml:"role,attr"`
}

type collation struct {
	URI     string `xml:"uri,attr"`
	Default string `xml:"default,attr"`
}

type decimalFormat struct {
	Name              string     `xml:"name,attr"`
	DecimalSeparator  string     `xml:"decimal-separator,attr"`
	GroupingSeparator string     `xml:"grouping-separator,attr"`
	Percent           string     `xml:"percent,attr"`
	PerMille          string     `xml:"per-mille,attr"`
	ZeroDigit         string     `xml:"zero-digit,attr"`
	Digit             string     `xml:"digit,attr"`
	PatternSeparator  string     `xml:"pattern-separator,attr"`
	ExponentSeparator string     `xml:"exponent-separator,attr"`
	Infinity          string     `xml:"infinity,attr"`
	NaN               string     `xml:"NaN,attr"`
	MinusSign         string     `xml:"minus-sign,attr"`
	Attrs             []xml.Attr `xml:",any,attr"`
}

type resource struct {
	File string `xml:"file,attr"`
	URI  string `xml:"uri,attr"`
}

type source struct {
	Role       string `xml:"role,attr"`
	File       string `xml:"file,attr"`
	URI        string `xml:"uri,attr"`
	Validation string `xml:"validation,attr"`
}

type collection struct {
	URI     string   `xml:"uri,attr"`
	Sources []source `xml:"source"`
	Query   string   `xml:"query"`
}

type namespace struct {
	Prefix string `xml:"prefix,attr"`
	URI    string `xml:"uri,attr"`
}

type param struct {
	Name     string `xml:"name,attr"`
	Select   string `xml:"select,attr"`
	As       string `xml:"as,attr"`
	Declared string `xml:"declared,attr"`
}

type sourceBinding struct {
	Name       string
	File       string
	URI        string
	Validation string
}

type schemaBinding struct {
	URI  string // target namespace ("" for a no-namespace schema)
	File string // .xsd path relative to the suite testdata root
}

type collectionBinding struct {
	URI        string
	SourceDocs []sourceBinding
	Query      string
}

type testCase struct {
	Name         string       `xml:"name,attr"`
	Test         string       `xml:"test"`
	Environment  *environment `xml:"environment"`
	Dependencies []dependency `xml:"dependency"`
	Result       resultSpec   `xml:",any"`
}

type dependency struct {
	Type      string `xml:"type,attr"`
	Value     string `xml:"value,attr"`
	Satisfied string `xml:"satisfied,attr"`
}

type resultSpec struct {
	XMLName xml.Name `xml:""`
	Inner   []byte   `xml:",innerxml"`
}

type assertion struct {
	Type           string
	Value          string
	NormalizeSpace bool
	Children       []assertion
}

// ──────────────────────────────────────────────────────────────────────
// Main
// ──────────────────────────────────────────────────────────────────────

// collectTests parses the QT3 catalog under sourceDir and returns the generated
// XPath test cases plus the sets of context-document and resource fixture files
// they reference (paths relative to sourceDir). It panics with a genErr —
// recovered at the Suite boundary — on a malformed catalog or unreadable
// test-set file.
func collectTests(sourceDir string) ([]generatedTest, map[string]bool, map[string]bool) {
	if _, err := os.Stat(filepath.Join(sourceDir, "catalog.xml")); os.IsNotExist(err) {
		genFatal("QT3 source not found. Run: go run ./cmd/w3cgen fetch qt3")
	}

	cat := parseCatalog(filepath.Join(sourceDir, "catalog.xml"))

	globalEnvs := make(map[string]*environment)
	for i := range cat.Environments {
		globalEnvs[cat.Environments[i].Name] = &cat.Environments[i]
	}

	var allTests []generatedTest
	docFiles := make(map[string]bool)
	resourceFiles := make(map[string]bool)

	for _, tsRef := range cat.TestSets {
		tsFile, err := generator.ContainedPath(sourceDir, tsRef.File)
		if err != nil {
			fmt.Printf("qt3gen: skipping unsafe test-set path %q: %v", tsRef.File, err)
			continue
		}
		ts := parseTestSet(tsFile)
		tsDir := filepath.Dir(tsRef.File)

		localEnvs := make(map[string]*environment)
		for i := range ts.Environments {
			localEnvs[ts.Environments[i].Name] = &ts.Environments[i]
		}

		// Skip entire test set if it requires XQuery at the set level
		if !isXPathApplicable(ts.Dependencies) {
			continue
		}

		setSkipReason := getTestSetSkipReason(tsRef.Name)

		for _, tc := range ts.TestCases {
			mergedDeps := mergeDeps(ts.Dependencies, tc.Dependencies)
			if !isXPathApplicable(mergedDeps) {
				continue
			}
			if hasFeatureDependency(mergedDeps, "xpath-1.0-compatibility") {
				continue
			}

			skipReason := getSkipReason(mergedDeps)
			if skipReason == "" {
				skipReason = getTestCaseSkipReason(tsRef.Name, tc.Name)
			}
			if skipReason == "" {
				skipReason = setSkipReason
			}
			env, envIsGlobal := resolveEnvironment(tc.Environment, localEnvs, globalEnvs)
			schemas := envSchemas(env, envIsGlobal, tsDir)

			if env != nil {
				if envSkip := checkEnvironmentSupport(env); envSkip != "" {
					if skipReason == "" {
						skipReason = envSkip
					}
				}
			}
			if skipReason == "" {
				skipReason = schemaAwareSkip(env, len(schemas) > 0)
			}
			// A subset of "requires static typing" cases raise the expected type
			// error DYNAMICALLY in helium's evaluator (or take an any-of branch
			// that holds), so the optional static-typing feature isn't actually
			// needed — un-skip them.
			if skipReason == "requires static typing" && qt3StaticTypingRaisesDynamically(tc.Name) {
				skipReason = ""
			}
			// A subset of xsd-version="1.0" cases assert a result that holds in
			// both XSD 1.0 and 1.1, so the 1.0 gate is not actually needed —
			// un-skip them (the genuine 1.0-vs-1.1 datatype/regex divergences
			// stay skipped).
			if skipReason == "requires XSD 1.0" && qt3XSD10VersionNeutral(tc.Name) {
				skipReason = ""
			}

			var contextDocPath string
			var contextValidation string
			if env != nil {
				for _, src := range env.Sources {
					if src.File == "" {
						continue
					}
					resolvedPath := resolveEnvSourcePath(envIsGlobal, tsDir, src.File)
					if src.Role == "." {
						contextDocPath = resolvedPath
						contextValidation = src.Validation
						docFiles[contextDocPath] = true
					}
					if strings.HasPrefix(src.Role, "$") {
						docFiles[resolvedPath] = true
					}
				}
				for _, col := range env.Collections {
					for _, src := range col.Sources {
						if src.File == "" {
							continue
						}
						docFiles[resolveEnvSourcePath(envIsGlobal, tsDir, src.File)] = true
					}
				}
			}

			for _, sc := range schemas {
				collectSchemaFiles(sourceDir, sc.File, docFiles)
			}

			assertions := parseResultAssertions(tc)

			var baseURI string
			if env != nil && env.StaticBaseURI != nil && env.StaticBaseURI.URI != "#UNDEFINED" {
				baseURI = env.StaticBaseURI.URI
			}

			// The FOTS static base URI of a test defaults to the URI of its
			// test-set document; the harness serves fixtures under
			// http://www.w3.org/fots/<path>. It is threaded to the fn:transform
			// adapter (WithTransformBaseURI) as the base for resolving a relative
			// stylesheet-location, so such a location resolves against the test-set
			// location where the fixtures live. Only an fn:transform case consults it,
			// so it is emitted only for the transform feature — a document-URI base on
			// every one of the ~22k cases would be dead weight. A test whose
			// environment declares the static base URI as #UNDEFINED keeps an
			// undefined (empty) base (a relative reference there must be unresolvable).
			var fotsBaseURI string
			usesTransform := hasFeatureDependency(mergedDeps, "fn-transform-XSLT") ||
				hasFeatureDependency(mergedDeps, "fn-transform-XSLT30")
			undefinedBase := env != nil && env.StaticBaseURI != nil && env.StaticBaseURI.URI == "#UNDEFINED"
			if usesTransform && !undefinedBase {
				fotsBaseURI = "http://www.w3.org/fots/" + filepath.ToSlash(tsRef.File)
			}

			// Detect resource environments (e.g., fn:json-doc/doc tests with URI-mapped files)
			needsHTTP := false
			var resMap map[string]string
			if env != nil && (len(env.Resources) > 0 || len(env.Sources) > 0 || len(env.Collections) > 0) {
				resMap = make(map[string]string)
				for _, res := range env.Resources {
					if res.File != "" && res.URI != "" {
						needsHTTP = true
						resPath := resolveEnvSourcePath(envIsGlobal, tsDir, res.File)
						resourceFiles[resPath] = true
						resMap[res.URI] = resPath
					}
				}
				for _, src := range env.Sources {
					if src.File != "" && src.URI != "" {
						needsHTTP = true
						srcPath := resolveEnvSourcePath(envIsGlobal, tsDir, src.File)
						resourceFiles[srcPath] = true
						resMap[src.URI] = srcPath
					}
				}
				for _, col := range env.Collections {
					for _, src := range col.Sources {
						if src.File == "" || src.URI == "" {
							continue
						}
						needsHTTP = true
						srcPath := resolveEnvSourcePath(envIsGlobal, tsDir, src.File)
						resourceFiles[srcPath] = true
						resMap[src.URI] = srcPath
					}
				}
				if len(resMap) == 0 {
					resMap = nil
				}
			}

			allTests = append(allTests, generatedTest{
				SetName:           tsRef.Name,
				CaseName:          tc.Name,
				XPath:             strings.TrimSpace(tc.Test),
				ContextDocPath:    contextDocPath,
				Namespaces:        collectNamespaces(env),
				DefaultLanguage:   dependencyValue(mergedDeps, "default-language"),
				DefaultCollation:  envDefaultCollation(env),
				DefaultDecimal:    envDefaultDecimalFormat(env),
				DecimalFormats:    envNamedDecimalFormats(env),
				Params:            envParams(env),
				Collections:       envCollections(env, envIsGlobal, tsDir),
				VariableSources:   envVariableSources(env, envIsGlobal, tsDir),
				BaseURI:           baseURI,
				FOTSBaseURI:       fotsBaseURI,
				NeedsHTTP:         needsHTTP,
				ResourceMap:       resMap,
				Schemas:           schemas,
				ContextValidation: contextValidation,
				SchemaVersion:     schemaVersionForDeps(mergedDeps),
				XML11:             hasXML11Dependency(mergedDeps),
				Assertions:        assertions,
				SkipReason:        skipReason,
			})
		}
	}

	return allTests, docFiles, resourceFiles
}

// copyDocs copies the referenced context-document and resource fixtures from
// sourceDir into destDir, preserving the suite-root-relative layout so that
// document()/fn:doc/fn:json-doc and collection lookups resolve at test time.
// Every path is validated through generator.ContainedPath before it is read or
// written. A source file absent upstream is warned and skipped; any other copy
// failure is fatal. It returns the number of files copied.
func copyDocs(sourceDir, destDir string, docFiles, resourceFiles map[string]bool) (int, error) {
	copied := 0
	for _, set := range []map[string]bool{docFiles, resourceFiles} {
		for rel := range set {
			srcFull, err := generator.ContainedPath(sourceDir, rel)
			if err != nil {
				return copied, fmt.Errorf("unsafe fixture source %q: %w", rel, err)
			}
			dstFull, err := generator.ContainedPath(destDir, rel)
			if err != nil {
				return copied, fmt.Errorf("unsafe fixture dest %q: %w", rel, err)
			}
			if err := copyAsset(srcFull, dstFull); err != nil {
				if os.IsNotExist(err) {
					fmt.Printf("warning: catalog fixture %q not present in source, skipping\n", rel)
					continue
				}
				return copied, fmt.Errorf("copy fixture %q: %w", rel, err)
			}
			copied++
		}
	}
	return copied, nil
}

// writeCategoryFiles groups the tests by catalog category and writes one
// xpath3/qt3_<category>_gen_test.go per category through generator.WriteGoFile
// (which formats and, in verify mode, checks for drift).
func writeCategoryFiles(outputDir string, allTests []generatedTest, mode generator.GenerateMode) error {
	categories := groupByCategory(allTests)
	for _, cat := range sortedCategoryNames(categories) {
		out := filepath.Join(outputDir, fmt.Sprintf("qt3_%s_gen_test.go", cat))
		if err := generator.WriteGoFile(out, generateTestFile(categories[cat]), mode); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// Parsing
// ──────────────────────────────────────────────────────────────────────

func parseCatalog(path string) *catalog {
	f, err := os.Open(path)
	if err != nil {
		genFatalf("opening catalog: %v", err)
	}
	defer func() { _ = f.Close() }()
	var c catalog
	dec := xml.NewDecoder(f)
	dec.CharsetReader = charset.NewReaderLabel
	if err := dec.Decode(&c); err != nil {
		genFatalf("parsing catalog: %v", err)
	}
	return &c
}

func parseTestSet(path string) *testSetFile {
	f, err := os.Open(path)
	if err != nil {
		genFatalf("opening test set %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var ts testSetFile
	dec := xml.NewDecoder(f)
	dec.CharsetReader = charset.NewReaderLabel
	if err := dec.Decode(&ts); err != nil {
		genFatalf("parsing test set %s: %v", path, err)
	}
	return &ts
}

// ──────────────────────────────────────────────────────────────────────
// Spec filtering
// ──────────────────────────────────────────────────────────────────────

// mergeDeps combines test-set-level and test-case-level dependencies.
// If a test case has its own spec dependency, it takes precedence over the
// test-set-level spec dependency. Other dependency types are concatenated.
func mergeDeps(setDeps, caseDeps []dependency) []dependency {
	caseHasSpec := false
	for _, d := range caseDeps {
		if d.Type == "spec" {
			caseHasSpec = true
			break
		}
	}

	merged := make([]dependency, 0, len(setDeps)+len(caseDeps))
	for _, d := range setDeps {
		if d.Type == "spec" && caseHasSpec {
			continue // test-case spec overrides test-set spec
		}
		merged = append(merged, d)
	}
	merged = append(merged, caseDeps...)
	return merged
}

func isXPathApplicable(deps []dependency) bool {
	// Exclude tests that require the ABSENCE of a feature we support (e.g.
	// unicode-normalization-form value="FULLY-NORMALIZED" satisfied="false", or
	// fn-transform-XSLT satisfied="false"). This must be a pre-pass: an
	// applicable spec token below returns true on first match, so a supported-
	// feature exclusion ordered after the spec dependency would otherwise never
	// fire.
	for _, d := range deps {
		if d.Satisfied == "false" && isSupportedFeature(d) { //nolint:goconst
			return false
		}
	}
	hasSpecDep := false
	for _, d := range deps {
		if d.Type != "spec" {
			continue
		}
		hasSpecDep = true
		for v := range strings.FieldsSeq(d.Value) {
			if !strings.HasPrefix(v, "XP") {
				continue
			}
			if d.Satisfied == "false" {
				continue
			}
			// We target XPath 3.1. Accept versions that include 3.1:
			// XP31, XP31+, XP30+, XP20+, XP10+ all cover 3.1.
			// Reject exact versions that exclude 3.1: XP10, XP20, XP30.
			if xpVersionIncludes31(v) {
				return true
			}
		}
	}
	return !hasSpecDep
}

// isSupportedFeature returns true if the dependency refers to a feature
// that this implementation supports. Used to exclude tests requiring the
// absence of such features (satisfied="false").
func isSupportedFeature(d dependency) bool {
	switch d.Type {
	case "unicode-normalization-form":
		switch d.Value {
		case "NFC", "NFD", "NFKC", "NFKD", "FULLY-NORMALIZED":
			return true
		}
	case "xsd-version":
		return d.Value == "1.1"
	case "feature":
		// helium's xslt3 fn:transform adapter supports both XSLT 1.0/2.0 and
		// XSLT 3.0 stylesheets, so a case that carries these features with
		// satisfied="false" — it targets a processor that does NOT support
		// fn:transform / XSLT 3.0 and expects the corresponding failure — is
		// dependency-excluded rather than run (its expected FOXT0004 failure
		// would be wrong for a supporting processor).
		switch d.Value {
		case "fn-transform-XSLT", "fn-transform-XSLT30":
			return true
		}
	}
	return false
}

// xpVersionIncludes31 returns true if the XPath spec token includes version 3.1.
// Tokens like "XP31", "XP31+", "XP30+", "XP20+" include 3.1.
// Tokens like "XP30", "XP20", "XP10" do not.
func xpVersionIncludes31(v string) bool {
	if strings.HasSuffix(v, "+") {
		return true // any "XPxx+" includes later versions
	}
	return v == "XP31" // exact match only for 3.1
}

func getSkipReason(deps []dependency) string {
	for _, d := range deps {
		if d.Type == "feature" && d.Satisfied != "false" {
			switch d.Value {
			// schemaImport / schemaAware / schemaValidation are served by wiring
			// the environment's XSD schema into the evaluator (SchemaDeclarations
			// + validated-source TypeAnnotations), so they run — but ONLY when the
			// environment actually declares a <schema> to compile (the
			// schema-presence gate schemaAwareSkip, applied after environment
			// resolution, keeps a schema-dependent case with no schema skipped).
			// A few schemaValidation cases genuinely need schema-validated
			// CONSTRUCTION semantics beyond type annotation (PSVI insignificant-
			// whitespace stripping of element-only content, or the XQuery
			// validate{} expression); those are pinned per-case in
			// getTestCaseSkipReason, not blanket-skipped here.
			case "moduleImport":
				return "requires XQuery module import"
			case "collection-stability":
				return "requires collection stability"
			case "directory-as-collection-uri":
				return "requires directory as collection URI"
			case "fn-load-xquery-module":
				return "requires XQuery load-xquery-module"
			case "remote_http":
				return "requires remote HTTP access"
			case "staticTyping":
				return "requires static typing"
			}
		}
		if d.Type == "unicode-version" && d.Value != qt3UnicodeVersion && d.Satisfied != "false" {
			return fmt.Sprintf("requires Unicode %s", d.Value)
		}
		if d.Type == "xsd-version" && d.Value == "1.0" && d.Satisfied != "false" {
			return "requires XSD 1.0"
		}
	}
	return ""
}

// getTestCaseSkipReason returns a skip reason for specific test cases that
// need per-case handling (e.g., tests expecting static typing errors).
func getTestCaseSkipReason(_, caseName string) string {
	switch caseName {
	// These tests pass () or integer where xs:string? is expected and expect XPTY0004.
	// Our dynamic evaluation handles these fine without static type checking.
	case "fn-unparsed-text-012", "fn-unparsed-text-available-008",
		"fn-unparsed-text-available-010", "fn-unparsed-text-available-012",
		"fn-unparsed-text-lines-012":
		return "requires static typing"

	// fn-transform-23 sets stylesheet-base-uri = string(base-uri($include)); the
	// env source $include has no uri attribute, so its base URI is the local parse
	// path rather than the http://www.w3.org/fots/ URL the resolver maps, and the
	// stylesheet's relative xsl:include href="render.xsl" resolves to an unmapped
	// URL (FOXT0003). Closing it needs the harness to give a no-uri env source the
	// FOTS document base URI, beyond the fn:transform adapter base wiring.
	case "fn-transform-23":
		return "fn:transform stylesheet-base-uri from base-uri() of a no-uri env source is the local parse path, so the relative xsl:include does not map to a fixture"

	// fn-transform-22 and fn-function-lookup-766a have only generic <assert>
	// result assertions, which the generator emits as a no-op (qt3AssertSkip),
	// so un-skipping them would produce a green false-pass that verifies nothing
	// but "evaluation did not error". Keep them skipped until the generator
	// supports generic <assert>. The "fn:transform" / "function-lookup"
	// substrings file them under the closeable "not-wired" class.
	case "fn-transform-22", "fn-function-lookup-766a":
		return "fn:transform/function-lookup assertions degrade to no-op (generic <assert>); pending generic-<assert> harness support"

	// fn:transform sub-feature gaps. The xslt3 fn:transform adapter (wired over
	// the xpath3 stub in qt3_helpers_test.go) runs the transform cases; the
	// following need behavior the adapter does not provide. All reasons carry the
	// "fn:transform" substring so qt3SkipClass files them under the closeable
	// "not-wired" class.

	// option validation on the empty default source: fn:transform-err-1 supplies
	// no source and no entry point; helium applies the stylesheet to an empty
	// default document rather than raising the FOXT0002 error the case expects.
	case "fn-transform-err-1":
		return "fn:transform applies to an empty default document instead of raising FOXT0002"

	}
	return ""
}

func getTestSetSkipReason(name string) string {
	return ""
}

// qt3StaticTypingRaisesDynamically lists "requires static typing" cases whose
// expected type error helium already raises at evaluation time (or whose any-of
// result branch helium already satisfies), so they pass without the optional
// static-typing feature and need not be skipped. Each was verified by evaluating
// its expression against helium's xpath3 engine. The remaining static-typing
// cases genuinely need compile-time analysis (they expect XPST0005 on an
// always-empty path, or guard the type mismatch behind an always-true condition
// so the offending branch is never evaluated) and stay skipped.
func qt3StaticTypingRaisesDynamically(caseName string) bool {
	switch caseName {
	case "fn-filter-006", "fn-filter-008", "fn-filter-009", "fn-filter-023",
		"fn-function-lookup-711", "fn-for-each-pair-010",
		"fn-unparsed-text-available-008", "fn-unparsed-text-available-010":
		return true
	}
	return false
}

// qt3XSD10VersionNeutral lists cases skipped as "requires XSD 1.0" whose sole
// assertion holds identically in XSD 1.0 and 1.1, so helium's XSD-1.1 datatype
// semantics satisfy them and the 1.0 gate is spurious. xs-double-005 casts a
// boundary subnormal to xs:double and asserts only assert-type xs:double, which
// is true in both versions. The remaining XSD-1.0 cases encode genuine
// 1.0-vs-1.1 divergences (+INF for double/float, relaxed xs:anyURI lexical
// space, year 0000 in gYear/gYearMonth, XSD-regex character-class dash rules)
// and stay skipped.
func qt3XSD10VersionNeutral(caseName string) bool {
	return caseName == "xs-double-005"
}

// ──────────────────────────────────────────────────────────────────────
// Environment resolution
// ──────────────────────────────────────────────────────────────────────

func resolveEnvironment(tcEnv *environment, local, global map[string]*environment) (*environment, bool) {
	if tcEnv == nil {
		return nil, false
	}
	if tcEnv.Ref != "" {
		if e, ok := local[tcEnv.Ref]; ok {
			return e, false
		}
		if e, ok := global[tcEnv.Ref]; ok {
			return e, true
		}
		return nil, false
	}
	return tcEnv, false
}

func checkEnvironmentSupport(env *environment) string {
	// A validation="strict"/"lax" source is handled at runtime: the source is
	// XSD-validated against the environment's schema and the resulting type
	// annotations are fed to the evaluator, so those environments run instead
	// of skipping — provided a schema is present (see schemaAwareSkip).
	for _, src := range env.Sources {
		if src.Role != "." && src.Role != "" && !strings.HasPrefix(src.Role, "$") {
			return fmt.Sprintf("requires source role %q", src.Role)
		}
	}
	return ""
}

// schemaAwareSkip gates schema-dependent cases on the presence of at least one
// <schema> binding in the resolved environment. A validation="strict"/"lax"
// source whose types must be annotated cannot run schema-aware without a schema
// to compile, so it stays skipped when hasSchema is false. When a schema is
// present the case runs (SchemaDeclarations + validated-source TypeAnnotations
// are wired at runtime). This is the shared gate used by both the generator
// (gen.go) and the manifest planner (plan.go) so the emitted case tables and
// the reported runnable/skipped counts agree.
//
// A schemaImport/schemaAware/schemaValidation FEATURE dependency is NOT gated
// here: in FOTS it is the spec's "schema-aware processor" conformance marker,
// not a requirement for a user-supplied compilable schema. helium supports the
// built-in list types (xs:ENTITIES/xs:IDREFS/xs:NMTOKENS) and the built-in
// json/analyze-string namespaces natively, so those cases run and their FOTS
// expected-result decides; the ones that genuinely need schema-validated PSVI
// type annotations are recorded as xfail in expectations/qt3.json.
func schemaAwareSkip(env *environment, hasSchema bool) string {
	if hasSchema {
		return ""
	}
	if env != nil {
		for _, src := range env.Sources {
			if src.Validation == "strict" || src.Validation == "lax" {
				return "requires XML Schema support"
			}
		}
	}
	return ""
}

func resolveEnvSourcePath(envIsGlobal bool, tsDir, file string) string {
	if envIsGlobal {
		return file
	}
	return filepath.Join(tsDir, file)
}

func envVariableSources(env *environment, envIsGlobal bool, tsDir string) []sourceBinding {
	if env == nil {
		return nil
	}

	var out []sourceBinding
	for _, src := range env.Sources {
		if src.File == "" || !strings.HasPrefix(src.Role, "$") {
			continue
		}
		out = append(out, sourceBinding{
			Name:       strings.TrimPrefix(src.Role, "$"),
			File:       resolveEnvSourcePath(envIsGlobal, tsDir, src.File),
			URI:        src.URI,
			Validation: src.Validation,
		})
	}
	return out
}

// envSchemas returns the schema files (with target namespaces) declared by the
// environment, resolved to suite-root-relative paths.
func envSchemas(env *environment, envIsGlobal bool, tsDir string) []schemaBinding {
	if env == nil {
		return nil
	}
	var out []schemaBinding
	for _, s := range env.Schemas {
		if s.File == "" {
			// A file-less role="import" of the well-known xpath-functions
			// namespace: the FOTS driver is expected to recognize the URI
			// (json-ns environment, W3C bug 28997). Bind the committed
			// schema-for-json.xsd overlay so schema-element(Q{…}map) resolves.
			if s.Role == "import" && s.URI == "http://www.w3.org/2005/xpath-functions" {
				out = append(out, schemaBinding{
					URI:  s.URI,
					File: "fn/json-to-xml/schema-for-json.xsd",
				})
			}
			continue
		}
		out = append(out, schemaBinding{
			URI:  s.URI,
			File: resolveEnvSourcePath(envIsGlobal, tsDir, s.File),
		})
	}
	return out
}

func envCollections(env *environment, envIsGlobal bool, tsDir string) []collectionBinding {
	if env == nil {
		return nil
	}

	var out []collectionBinding
	for _, col := range env.Collections {
		binding := collectionBinding{
			URI:   col.URI,
			Query: strings.TrimSpace(col.Query),
		}
		for _, src := range col.Sources {
			if src.File == "" {
				continue
			}
			// LIMITATION: a collection source's validation="strict"/"lax" is not
			// propagated here, so its nodes are parsed without XSD type
			// annotations (unlike context/variable sources). The FOTS suite
			// declares no schema-validated collection source (verified), so this
			// drops no runnable case; wire Validation through sourceBinding and
			// annotate the resolved collection nodes if one is ever added.
			binding.SourceDocs = append(binding.SourceDocs, sourceBinding{
				File: resolveEnvSourcePath(envIsGlobal, tsDir, src.File),
				URI:  src.URI,
			})
		}
		out = append(out, binding)
	}
	return out
}

func collectNamespaces(env *environment) map[string]string {
	if env == nil || len(env.Namespaces) == 0 {
		return nil
	}
	ns := make(map[string]string)
	for _, n := range env.Namespaces {
		ns[n.Prefix] = n.URI
	}
	return ns
}

func envDefaultCollation(env *environment) string {
	if env == nil || len(env.Collations) == 0 {
		return ""
	}
	for _, c := range env.Collations {
		if c.Default == "true" {
			return c.URI
		}
	}
	if len(env.Collations) == 1 {
		return env.Collations[0].URI
	}
	return ""
}

func envDefaultDecimalFormat(env *environment) *decimalFormat {
	if env == nil {
		return nil
	}
	for _, df := range env.DecimalFormat {
		if strings.TrimSpace(df.Name) == "" {
			cp := df
			return &cp
		}
	}
	return nil
}

type namedDecimalFormat struct {
	URI    string
	Name   string
	Format decimalFormat
}

func envParams(env *environment) []param {
	if env == nil || len(env.Params) == 0 {
		return nil
	}
	out := make([]param, len(env.Params))
	copy(out, env.Params)
	return out
}

func envNamedDecimalFormats(env *environment) []namedDecimalFormat {
	if env == nil || len(env.DecimalFormat) == 0 {
		return nil
	}

	ns := collectNamespaces(env)
	var out []namedDecimalFormat
	for _, df := range env.DecimalFormat {
		name := strings.TrimSpace(df.Name)
		if name == "" {
			continue
		}
		dfNS := make(map[string]string, len(ns))
		maps.Copy(dfNS, ns)
		maps.Copy(dfNS, decimalFormatNamespaces(df))
		uri, local, ok := resolveEnvQName(name, dfNS)
		if !ok {
			continue
		}
		out = append(out, namedDecimalFormat{
			URI:    uri,
			Name:   local,
			Format: df,
		})
	}
	return out
}

func decimalFormatNamespaces(df decimalFormat) map[string]string {
	if len(df.Attrs) == 0 {
		return nil
	}
	ns := make(map[string]string)
	for _, attr := range df.Attrs {
		switch {
		case attr.Name.Space == "xmlns":
			ns[attr.Name.Local] = attr.Value
		case attr.Name.Space == "" && attr.Name.Local == "xmlns":
			ns[""] = attr.Value
		}
	}
	return ns
}

func resolveEnvQName(name string, ns map[string]string) (string, string, bool) {
	if strings.HasPrefix(name, "Q{") {
		end := strings.Index(name, "}")
		if end < 0 || end == len(name)-1 {
			return "", "", false
		}
		return name[2:end], name[end+1:], true
	}

	if prefix, local, ok := strings.Cut(name, ":"); ok {
		uri := ns[prefix]
		if uri == "" {
			return "", "", false
		}
		return uri, local, true
	}

	return "", name, true
}

func dependencyValue(deps []dependency, typ string) string {
	for _, d := range deps {
		if d.Type != typ || d.Satisfied == "false" {
			continue
		}
		return d.Value
	}
	return ""
}

// schemaVersionForDeps returns "1.0" when the test carries an
// xsd-version="1.0" dependency (so its schema is compiled as XSD 1.0), or ""
// for the default (XSD 1.1).
func schemaVersionForDeps(deps []dependency) string {
	for _, d := range deps {
		if d.Type == "xsd-version" && d.Value == "1.0" && d.Satisfied != "false" {
			return "1.0"
		}
	}
	return ""
}

// collectSchemaFiles adds schemaFile (relative to sourceDir) and every schema
// it transitively references via xs:include / xs:import / xs:redefine
// schemaLocation to set, so Fetch copies the whole schema closure into the
// testdata tree. Paths that cannot be read are added anyway (copyDocs warns and
// skips a genuinely-absent file); resolution is best-effort and never fatal.
func collectSchemaFiles(sourceDir, schemaFile string, set map[string]bool) {
	collectSchemaFilesRec(sourceDir, schemaFile, set, map[string]bool{})
}

func collectSchemaFilesRec(sourceDir, schemaFile string, set, visited map[string]bool) {
	clean := filepath.ToSlash(filepath.Clean(schemaFile))
	if visited[clean] {
		return
	}
	visited[clean] = true
	set[schemaFile] = true

	full, err := generator.ContainedPath(sourceDir, schemaFile)
	if err != nil {
		return
	}
	data, err := os.ReadFile(full) //nolint:gosec // trusted committed suite source
	if err != nil {
		return
	}
	dir := filepath.Dir(schemaFile)
	for _, loc := range schemaLocations(data) {
		if loc == "" || strings.Contains(loc, "://") {
			continue
		}
		ref := filepath.Join(dir, filepath.FromSlash(loc))
		collectSchemaFilesRec(sourceDir, ref, set, visited)
	}
}

// schemaLocations extracts the schemaLocation attribute values of every
// xs:include / xs:import / xs:redefine / xs:override element in an XSD document.
func schemaLocations(data []byte) []string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.CharsetReader = charset.NewReaderLabel
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "include", "import", "redefine", "override":
		default:
			continue
		}
		for _, a := range se.Attr {
			if a.Name.Local == "schemaLocation" {
				out = append(out, a.Value)
			}
		}
	}
	return out
}

func hasFeatureDependency(deps []dependency, value string) bool {
	for _, d := range deps {
		if d.Type == "feature" && d.Satisfied != "false" && d.Value == value {
			return true
		}
	}
	return false
}

// hasXML11Dependency reports whether the case declares an xml-version="1.1"
// dependency. helium's parser already honors the 1.1 relaxations these cases
// exercise (version decl, prefix undeclaration, control-char refs); the runner
// builds the evaluator with AllowXML11Chars() so codepoints-to-string accepts
// the 1.1 restricted characters.
func hasXML11Dependency(deps []dependency) bool {
	for _, d := range deps {
		if d.Type == "xml-version" && d.Value == "1.1" && d.Satisfied != "false" {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────
// Assertion parsing
// ──────────────────────────────────────────────────────────────────────

func parseResultAssertions(tc testCase) []assertion {
	resultXML := "<result xmlns=\"" + qt3NS + "\">" + string(tc.Result.Inner) + "</result>"
	return parseAssertionXML(resultXML)
}

type xmlResult struct {
	Children []xmlAssertion `xml:",any"`
}

type xmlAssertion struct {
	XMLName        xml.Name       `xml:""`
	Code           string         `xml:"code,attr"`
	NormalizeSpace string         `xml:"normalize-space,attr"`
	Inner          []byte         `xml:",innerxml"`
	Children       []xmlAssertion `xml:",any"`
}

func parseAssertionXML(s string) []assertion {
	var result xmlResult
	if err := xml.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	var out []assertion
	for _, child := range result.Children {
		out = append(out, convertAssertion(child))
	}
	return out
}

func convertAssertion(xa xmlAssertion) assertion {
	// Decode the raw inner XML to preserve character references like &#xD;
	// that Go's xml:",chardata" would normalize away.
	value := decodeXMLText(string(xa.Inner))
	if xa.XMLName.Local != "assert-string-value" {
		value = strings.TrimSpace(value)
	}
	a := assertion{
		Type:           xa.XMLName.Local,
		Value:          value,
		NormalizeSpace: xa.NormalizeSpace == "true",
	}
	if xa.Code != "" {
		a.Value = xa.Code
	}
	for _, child := range xa.Children {
		a.Children = append(a.Children, convertAssertion(child))
	}
	return a
}

// ──────────────────────────────────────────────────────────────────────
// Code generation (table-driven)
// ──────────────────────────────────────────────────────────────────────

type generatedTest struct {
	SetName           string
	CaseName          string
	XPath             string
	ContextDocPath    string
	Namespaces        map[string]string
	DefaultLanguage   string
	DefaultCollation  string
	DefaultDecimal    *decimalFormat
	DecimalFormats    []namedDecimalFormat
	Params            []param
	Collections       []collectionBinding
	VariableSources   []sourceBinding
	BaseURI           string
	FOTSBaseURI       string
	NeedsHTTP         bool
	ResourceMap       map[string]string // URI → file path relative to testdata dir
	Schemas           []schemaBinding   // in-scope XSD schemas for schema-aware evaluation
	ContextValidation string            // "strict"/"lax"/"" for the context document
	SchemaVersion     string            // "1.0" to force XSD 1.0 compilation; "" = default 1.1
	XML11             bool              // xml-version="1.1" dependency: run under AllowXML11Chars()
	Assertions        []assertion
	SkipReason        string
}

func generateTestFile(tests []generatedTest) string {
	var b strings.Builder

	b.WriteString("// Code generated by w3cgen; DO NOT EDIT.\n\n")
	b.WriteString("package xpath3_test\n\n")

	// All cases across the category register into the shared qt3AllCases slice;
	// the single TestQT3W3C entry point (see the harness) runs them as subtests,
	// so every case surfaces under one root test for the JUnit reporter
	// regardless of its originating test-set.
	b.WriteString("func init() {\n")
	b.WriteString("\tqt3AllCases = append(qt3AllCases, []qt3Test{\n")

	{
		for _, tc := range tests {
			b.WriteString("\t\t{")
			fmt.Fprintf(&b, "Name: %q, ", tc.CaseName)
			fmt.Fprintf(&b, "XPath: %s", goStringLiteral(tc.XPath))

			if tc.ContextDocPath != "" {
				fmt.Fprintf(&b, ", DocPath: %q", tc.ContextDocPath)
			}
			if tc.DefaultLanguage != "" {
				fmt.Fprintf(&b, ", DefaultLanguage: %q", tc.DefaultLanguage)
			}
			if tc.DefaultCollation != "" {
				fmt.Fprintf(&b, ", DefaultCollation: %q", tc.DefaultCollation)
			}
			if tc.DefaultDecimal != nil {
				fmt.Fprintf(&b, ", DefaultDecimal: &%s", emitDecimalFormat(*tc.DefaultDecimal))
			}
			if len(tc.DecimalFormats) > 0 {
				b.WriteString(", NamedDecimalFormats: []qt3NamedDecimalFormat{")
				for i, df := range tc.DecimalFormats {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{URI: %q, Name: %q, Format: %s}", df.URI, df.Name, emitDecimalFormat(df.Format))
				}
				b.WriteString("}")
			}
			if len(tc.Params) > 0 {
				b.WriteString(", Params: []qt3Param{")
				for i, p := range tc.Params {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{Name: %q, Select: %q}", p.Name, p.Select)
				}
				b.WriteString("}")
			}
			if len(tc.VariableSources) > 0 {
				b.WriteString(", SourceDocs: []qt3SourceDoc{")
				for i, src := range tc.VariableSources {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{Name: %q, DocPath: %q", src.Name, src.File)
					if src.URI != "" {
						fmt.Fprintf(&b, ", URI: %q", src.URI)
					}
					if src.Validation != "" {
						fmt.Fprintf(&b, ", Validation: %q", src.Validation)
					}
					b.WriteString("}")
				}
				b.WriteString("}")
			}
			if len(tc.Collections) > 0 {
				b.WriteString(", Collections: []qt3Collection{")
				for i, col := range tc.Collections {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{URI: %q", col.URI)
					if len(col.SourceDocs) > 0 {
						b.WriteString(", SourceDocs: []qt3SourceDoc{")
						for j, src := range col.SourceDocs {
							if j > 0 {
								b.WriteString(", ")
							}
							fmt.Fprintf(&b, "{DocPath: %q", src.File)
							if src.URI != "" {
								fmt.Fprintf(&b, ", URI: %q", src.URI)
							}
							b.WriteString("}")
						}
						b.WriteString("}")
					}
					if col.Query != "" {
						fmt.Fprintf(&b, ", Query: %q", col.Query)
					}
					b.WriteString("}")
				}
				b.WriteString("}")
			}
			if tc.BaseURI != "" {
				fmt.Fprintf(&b, ", BaseURI: %q", tc.BaseURI)
			}
			if tc.FOTSBaseURI != "" {
				fmt.Fprintf(&b, ", FOTSBaseURI: %q", tc.FOTSBaseURI)
			}
			if tc.NeedsHTTP {
				b.WriteString(", NeedsHTTP: true")
			}
			if len(tc.ResourceMap) > 0 {
				b.WriteString(", ResourceMap: map[string]string{")
				keys := sortedKeys(tc.ResourceMap)
				for i, k := range keys {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "%q: %q", k, tc.ResourceMap[k])
				}
				b.WriteString("}")
			}
			if len(tc.Namespaces) > 0 {
				b.WriteString(", Namespaces: map[string]string{")
				keys := sortedKeys(tc.Namespaces)
				for i, k := range keys {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "%q: %q", k, tc.Namespaces[k])
				}
				b.WriteString("}")
			}
			if len(tc.Schemas) > 0 {
				b.WriteString(", Schemas: []qt3Schema{")
				for i, sc := range tc.Schemas {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{URI: %q, DocPath: %q}", sc.URI, sc.File)
				}
				b.WriteString("}")
			}
			if tc.ContextValidation != "" {
				fmt.Fprintf(&b, ", ContextValidation: %q", tc.ContextValidation)
			}
			if tc.SchemaVersion != "" {
				fmt.Fprintf(&b, ", SchemaVersion: %q", tc.SchemaVersion)
			}
			if tc.XML11 {
				b.WriteString(", XML11: true")
			}
			if tc.SkipReason != "" {
				fmt.Fprintf(&b, ", Skip: %q", tc.SkipReason)
			}
			if assertionsExpectError(tc.Assertions) {
				b.WriteString(", ExpectError: true")
			} else {
				if assertionsAcceptError(tc.Assertions) {
					b.WriteString(", AcceptError: true")
				}
				assertExprs := emitAssertions(tc.Assertions)
				if len(assertExprs) > 0 {
					b.WriteString(", Assertions: []qt3Assertion{")
					b.WriteString(strings.Join(assertExprs, ", "))
					b.WriteString("}")
				}
			}

			b.WriteString("},\n")
		}
	}

	b.WriteString("\t}...)\n")
	b.WriteString("}\n")

	return b.String()
}

func assertionsExpectError(assertions []assertion) bool {
	for _, a := range assertions {
		if a.Type == "error" { //nolint:goconst
			return true
		}
		if a.Type == "any-of" { // Only treat as error-only if ALL children are errors.
			// If any-of has both error and non-error children,
			// the non-error result is also acceptable (XP31 behavior).
			allError := true
			for _, child := range a.Children {
				if child.Type != "error" {
					allError = false
					break
				}
			}
			if allError {
				return true
			}
		}
	}
	return false
}

// assertionsAcceptError returns true when any-of contains both error and non-error children.
// In this case, an error is acceptable but a valid result should also be checked.
func assertionsAcceptError(assertions []assertion) bool {
	for _, a := range assertions {
		if a.Type == "any-of" {
			hasError := false
			hasNonError := false
			for _, child := range a.Children {
				if child.Type == "error" {
					hasError = true
				} else {
					hasNonError = true
				}
			}
			if hasError && hasNonError {
				return true
			}
		}
	}
	return false
}

// emitAssertions returns Go expressions for assertion values.
func emitAssertions(assertions []assertion) []string {
	var out []string
	for _, a := range assertions {
		out = append(out, emitAssertion(a)...)
	}
	return out
}

// emitAssertion returns one or more Go expressions (all-of expands to multiple).
func emitAssertion(a assertion) []string {
	switch a.Type {
	case "assert-eq":
		return []string{fmt.Sprintf("qt3AssertEq(%s)", goStringLiteral(a.Value))}
	case "assert-string-value":
		if a.NormalizeSpace {
			return []string{fmt.Sprintf("qt3AssertStringValueNS(%s)", goStringLiteral(a.Value))}
		}
		return []string{fmt.Sprintf("qt3AssertStringValue(%s)", goStringLiteral(a.Value))}
	case "assert-true":
		return []string{"qt3AssertTrue()"}
	case "assert-false":
		return []string{"qt3AssertFalse()"}
	case "assert-empty":
		return []string{"qt3AssertEmpty()"}
	case "assert-count":
		n, _ := strconv.Atoi(a.Value)
		return []string{fmt.Sprintf("qt3AssertCount(%d)", n)}
	case "assert-type":
		return []string{fmt.Sprintf("qt3AssertType(%q)", a.Value)}
	case "assert-deep-eq":
		return []string{fmt.Sprintf("qt3AssertDeepEq(%s)", goStringLiteral(a.Value))}
	case "assert-xml", "assert-permutation", "assert-serialization-error", "assert-serialization":
		return []string{"qt3AssertSkip()"}
	case "all-of":
		return emitAssertions(a.Children)
	case "any-of":
		var checks []string
		for _, child := range a.Children {
			if child.Type == "error" {
				continue // handled by assertionsExpectError
			}
			checks = append(checks, emitCheck(child))
		}
		if len(checks) == 0 {
			return nil
		}
		return []string{fmt.Sprintf("qt3AnyOf(%s)", strings.Join(checks, ", "))}
	case "error":
		return nil // handled separately
	default:
		return []string{"qt3AssertSkip()"}
	}
}

// emitCheck returns a Go expression for a qt3Check value (used in any-of).
func emitCheck(a assertion) string {
	switch a.Type {
	case "assert-eq":
		return fmt.Sprintf("qt3CheckEq(%s)", goStringLiteral(a.Value))
	case "assert-string-value":
		if a.NormalizeSpace {
			return fmt.Sprintf("qt3CheckStringValueNS(%s)", goStringLiteral(a.Value))
		}
		return fmt.Sprintf("qt3CheckStringValue(%s)", goStringLiteral(a.Value))
	case "assert-true":
		return "qt3CheckTrue()"
	case "assert-false":
		return "qt3CheckFalse()"
	case "assert-empty":
		return "qt3CheckEmpty()"
	case "assert-count":
		n, _ := strconv.Atoi(a.Value)
		return fmt.Sprintf("qt3CheckCount(%d)", n)
	case "assert-type":
		return fmt.Sprintf("qt3CheckType(%q)", a.Value)
	case "assert-deep-eq":
		return fmt.Sprintf("qt3CheckDeepEq(%s)", goStringLiteral(a.Value))
	default:
		return "qt3CheckSkip()"
	}
}

func emitDecimalFormat(df decimalFormat) string {
	var parts []string
	if df.DecimalSeparator != "" {
		parts = append(parts, fmt.Sprintf("DecimalSeparator: %s", goStringLiteral(df.DecimalSeparator)))
	}
	if df.GroupingSeparator != "" {
		parts = append(parts, fmt.Sprintf("GroupingSeparator: %s", goStringLiteral(df.GroupingSeparator)))
	}
	if df.Percent != "" {
		parts = append(parts, fmt.Sprintf("Percent: %s", goStringLiteral(df.Percent)))
	}
	if df.PerMille != "" {
		parts = append(parts, fmt.Sprintf("PerMille: %s", goStringLiteral(df.PerMille)))
	}
	if df.ZeroDigit != "" {
		parts = append(parts, fmt.Sprintf("ZeroDigit: %s", goStringLiteral(df.ZeroDigit)))
	}
	if df.Digit != "" {
		parts = append(parts, fmt.Sprintf("Digit: %s", goStringLiteral(df.Digit)))
	}
	if df.PatternSeparator != "" {
		parts = append(parts, fmt.Sprintf("PatternSeparator: %s", goStringLiteral(df.PatternSeparator)))
	}
	if df.ExponentSeparator != "" {
		parts = append(parts, fmt.Sprintf("ExponentSeparator: %s", goStringLiteral(df.ExponentSeparator)))
	}
	if df.Infinity != "" {
		parts = append(parts, fmt.Sprintf("Infinity: %s", goStringLiteral(df.Infinity)))
	}
	if df.NaN != "" {
		parts = append(parts, fmt.Sprintf("NaN: %s", goStringLiteral(df.NaN)))
	}
	if df.MinusSign != "" {
		parts = append(parts, fmt.Sprintf("MinusSign: %s", goStringLiteral(df.MinusSign)))
	}
	return "qt3DecimalFormat{" + strings.Join(parts, ", ") + "}"
}

// ──────────────────────────────────────────────────────────────────────
// Utilities
// ──────────────────────────────────────────────────────────────────────

func goStringLiteral(s string) string {
	// Raw string literals (backtick) silently discard \r, so always use
	// interpreted string literals when the value contains CR.
	if strings.Contains(s, "\n") && !strings.Contains(s, "`") && !strings.Contains(s, "\r") {
		return "`" + s + "`"
	}
	return fmt.Sprintf("%q", s)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// categoryOf extracts the grouping prefix from a test set name.
// e.g. "fn-abs" → "fn", "op-numeric-add" → "op", "prod-AxisStep" → "prod"
func categoryOf(setName string) string {
	if idx := strings.IndexByte(setName, '-'); idx > 0 {
		return setName[:idx]
	}
	return setName
}

func groupByCategory(tests []generatedTest) map[string][]generatedTest {
	cats := make(map[string][]generatedTest)
	for _, t := range tests {
		cat := categoryOf(t.SetName)
		cats[cat] = append(cats[cat], t)
	}
	return cats
}

func sortedCategoryNames(cats map[string][]generatedTest) []string {
	names := make([]string, 0, len(cats))
	for name := range cats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
