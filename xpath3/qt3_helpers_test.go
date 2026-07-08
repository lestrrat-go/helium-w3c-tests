package xpath3_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lestrrat-go/helium"
	"github.com/lestrrat-go/helium/xpath3"
	"github.com/lestrrat-go/helium/xsd"
	"github.com/lestrrat-go/helium/xslt3"
	"github.com/stretchr/testify/require"
)

// qt3Expectations is the hand-authored skip/xfail contract read from
// expectations/qt3.json. Skip force-skips a case by name (a hand override on top
// of the generator's dependency-derived Skip). XFail names cases helium should
// support but currently diverges on: the case runs, is expected to fail, and an
// unexpected PASS is a hard error (mirrors the xsd10 harness), so a divergence
// that gets fixed can never be silently left green-listed.
type qt3Expectations struct {
	Skip  map[string]string `json:"skip"`
	XFail map[string]string `json:"xfail"`
}

var (
	qt3ExpectationsOnce sync.Once
	qt3ExpectationsData qt3Expectations
)

// qt3LoadExpectations loads expectations/qt3.json exactly once. QT3_EXPECTATIONS
// overrides the default path so parallel chunks can each point at their own
// copy. A missing file yields empty maps; a present-but-malformed file panics (a
// silent parse failure would disable every hand skip/xfail and surface as
// confusing test outcomes).
func qt3LoadExpectations() qt3Expectations {
	qt3ExpectationsOnce.Do(func() {
		p := os.Getenv("QT3_EXPECTATIONS")
		override := p != ""
		if !override {
			p = filepath.Join("..", "expectations", "qt3.json")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			// Only the default path may legitimately be absent (e.g. an unusual
			// CWD) → empty maps. An explicit QT3_EXPECTATIONS override that
			// can't be read is operator error (a typo, or a relative path that
			// resolved against the package dir): fail loudly rather than
			// silently disabling every hand skip/xfail, which would make the
			// unexpected-pass tripwire vacuous.
			if os.IsNotExist(err) && !override {
				return
			}
			panic(fmt.Sprintf("read expectations %s: %v", p, err))
		}
		if err := json.Unmarshal(data, &qt3ExpectationsData); err != nil {
			panic(fmt.Sprintf("parse expectations %s: %v", p, err))
		}
	})
	return qt3ExpectationsData
}

// qt3TB is the subset of *testing.T the result-check phase and the assertion
// factories use. Abstracting it lets an xfail case run against a recorder that
// captures failures instead of propagating them to the go test runner, while
// the normal path passes the real *testing.T for byte-identical behavior.
type qt3TB interface {
	require.TestingT // Errorf(format string, args ...any); FailNow()
	Helper()
	Fatalf(format string, args ...any)
	Context() context.Context
}

// qt3Recorder is a qt3TB that records failures instead of reporting them, used
// to run an xfail case and detect whether it (unexpectedly) passed. FailNow and
// Fatalf call runtime.Goexit exactly like *testing.T, so a case body must run in
// a dedicated goroutine (see qt3RunXFail).
type qt3Recorder struct {
	ctx    context.Context
	failed bool
	msgs   []string
}

func (r *qt3Recorder) Helper()                  {}
func (r *qt3Recorder) Context() context.Context { return r.ctx }
func (r *qt3Recorder) FailNow()                 { r.failed = true; runtime.Goexit() }
func (r *qt3Recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *qt3Recorder) Fatalf(format string, args ...any) { r.Errorf(format, args...); runtime.Goexit() }

// qt3Resolver returns a URIResolver that lets QT3 tests reach both the
// shared HTTP fixture server and the local testdata tree. It's the QT3
// harness's explicit opt-in to network + filesystem access.
func qt3Resolver(client *http.Client) xpath3.URIResolver {
	return &qt3CombinedResolver{
		httpR: &qt3HTTPResolver{client: client},
		fileR: &qt3FileURIResolver{baseDir: string(filepath.Separator)},
	}
}

// qt3TransformBaseURI is the static base URI handed to the fn:transform adapter,
// used to resolve a relative stylesheet-location (a relative stylesheet-location
// resolves against the transform call's static base URI) and to make a relative
// stylesheet-base-uri option absolute (fn-transform-err-9a). An explicit
// environment static-base-uri wins; otherwise the FOTS test-set document URI is
// used, which is where the fixtures live.
//
// The one case the base is withheld: a stylesheet-text with no
// stylesheet-base-uri option. xslt3 falls back to the transform call's static
// base URI as the base of an inline stylesheet-text, so supplying it would let a
// relative xsl:include resolve even though the case supplies no
// stylesheet-base-uri and expects the include to be unresolvable
// (fn-transform-err-9, which asserts XTSE0165). Withholding the base only in
// that shape keeps err-9 correct while a stylesheet-text case that DOES supply
// stylesheet-base-uri (err-9a) still resolves its include against it.
func qt3TransformBaseURI(tc qt3Test) string {
	if tc.BaseURI != "" {
		return tc.BaseURI
	}
	if tc.FOTSBaseURI != "" {
		if strings.Contains(tc.XPath, "stylesheet-text") && !strings.Contains(tc.XPath, "stylesheet-base-uri") {
			return qt3DefaultBaseURI(tc)
		}
		return tc.FOTSBaseURI
	}
	return qt3DefaultBaseURI(tc)
}

// qt3TransformMutator registers the real xslt3 fn:transform over the xpath3 stub.
// User functions take precedence over builtins, so this cleanly overrides the
// {fn}transform stub for the QT3 harness. The adapter inherits the harness's
// combined HTTP+filesystem resolver and shared HTTP client.
func qt3TransformMutator(client *http.Client, baseURI string) qt3EvalMutator {
	return func(e xpath3.Evaluator) xpath3.Evaluator {
		transformOpts := []xslt3.TransformOption{
			xslt3.WithTransformURIResolver(qt3Resolver(client)),
			xslt3.WithTransformHTTPClient(client),
		}
		if baseURI != "" {
			transformOpts = append(transformOpts, xslt3.WithTransformBaseURI(baseURI))
		}
		fn := xslt3.TransformFunction(transformOpts...)
		return e.Functions(nil, map[xpath3.QualifiedName]xpath3.Function{
			{URI: xpath3.NSFn, Name: "transform"}: fn,
		})
	}
}

type qt3CombinedResolver struct {
	httpR xpath3.URIResolver
	fileR xpath3.URIResolver
}

func (r *qt3CombinedResolver) ResolveURI(uri string) (io.ReadCloser, error) {
	parsed, err := url.Parse(uri)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return r.httpR.ResolveURI(uri)
	}
	return r.fileR.ResolveURI(uri)
}

// qt3Assertion checks a result sequence, calling t.Fatal on failure.
type qt3Assertion func(t qt3TB, seq xpath3.Sequence)

// qt3Check returns true if a result sequence satisfies a condition (for any-of).
type qt3Check func(seq xpath3.Sequence) bool
type qt3EvalMutator func(xpath3.Evaluator) xpath3.Evaluator

type qt3Param struct {
	Name   string
	Select string
}

type qt3SourceDoc struct {
	Name       string
	DocPath    string
	URI        string
	Validation string // "strict"/"lax"/"" — XSD-validate this source and annotate its types
}

// qt3Schema is an in-scope XSD schema for a schema-aware test: URI is the
// schema's target namespace, DocPath the .xsd path relative to the testdata dir.
type qt3Schema struct {
	URI     string
	DocPath string
}

type qt3Collection struct {
	URI        string
	SourceDocs []qt3SourceDoc
	Query      string
}

type qt3Test struct {
	Name                string
	XPath               string
	DocPath             string // relative to qt3TestDataDir(); empty = no context document
	Namespaces          map[string]string
	DefaultLanguage     string
	DefaultCollation    string
	DefaultDecimal      *qt3DecimalFormat
	NamedDecimalFormats []qt3NamedDecimalFormat
	Params              []qt3Param
	SourceDocs          []qt3SourceDoc
	Collections         []qt3Collection
	BaseURI             string            // static base URI for fn:unparsed-text etc.
	FOTSBaseURI         string            // FOTS test-set document URI; fn:transform-adapter base for relative stylesheet-location resolution only (never the global evaluator base)
	NeedsHTTP           bool              // test requires HTTP client (e.g. fn:json-doc with URL)
	ResourceMap         map[string]string // URI → file path (relative to qt3TestDataDir()) for resource resolution
	Schemas             []qt3Schema       // in-scope XSD schemas for schema-aware evaluation
	ContextValidation   string            // "strict"/"lax"/"" — validate + annotate the context document
	SchemaVersion       string            // "1.0" forces XSD 1.0 schema compilation; "" = default 1.1
	XML11               bool              // run under AllowXML11Chars() (xml-version="1.1" dependency)
	Skip                string
	ExpectError         bool
	AcceptError         bool // error is acceptable but not required (any-of with error + non-error)
	FalsePassRisk       bool // RUN case with no real assertion and no expected error (a green no-op); locked by TestQT3WeakNoOpGuard
	Assertions          []qt3Assertion
}

type qt3DecimalFormat struct {
	DecimalSeparator  string
	GroupingSeparator string
	Percent           string
	PerMille          string
	ZeroDigit         string
	Digit             string
	PatternSeparator  string
	ExponentSeparator string
	Infinity          string
	NaN               string
	MinusSign         string
}

type qt3NamedDecimalFormat struct {
	URI    string
	Name   string
	Format qt3DecimalFormat
}

type qt3CollectionResolver struct {
	collections    map[string]xpath3.Sequence
	uriCollections map[string][]string
}

func (r *qt3CollectionResolver) ResolveCollection(uri string) (xpath3.Sequence, error) {
	seq, ok := r.collections[uri]
	if !ok {
		return nil, fmt.Errorf("collection %q not found", uri)
	}
	return xpath3.ItemSlice(append([]xpath3.Item(nil), seq.Materialize()...)), nil
}

func (r *qt3CollectionResolver) ResolveURICollection(uri string) ([]string, error) {
	uris, ok := r.uriCollections[uri]
	if !ok {
		return nil, fmt.Errorf("uri-collection %q not found", uri)
	}
	return append([]string(nil), uris...), nil
}

func qt3ApplyEval(eval xpath3.Evaluator, muts []qt3EvalMutator) xpath3.Evaluator {
	for _, mut := range muts {
		eval = mut(eval)
	}
	return eval
}

func (df qt3DecimalFormat) toXPath3() xpath3.DecimalFormat {
	out := xpath3.DefaultDecimalFormat()
	if df.DecimalSeparator != "" {
		out.DecimalSeparator = qt3SingleRune(df.DecimalSeparator)
	}
	if df.GroupingSeparator != "" {
		out.GroupingSeparator = qt3SingleRune(df.GroupingSeparator)
	}
	if df.Percent != "" {
		out.Percent = qt3SingleRune(df.Percent)
	}
	if df.PerMille != "" {
		out.PerMille = qt3SingleRune(df.PerMille)
	}
	if df.ZeroDigit != "" {
		out.ZeroDigit = qt3SingleRune(df.ZeroDigit)
	}
	if df.Digit != "" {
		out.Digit = qt3SingleRune(df.Digit)
	}
	if df.PatternSeparator != "" {
		out.PatternSeparator = qt3SingleRune(df.PatternSeparator)
	}
	if df.ExponentSeparator != "" {
		out.ExponentSeparator = qt3SingleRune(df.ExponentSeparator)
	}
	if df.Infinity != "" {
		out.Infinity = df.Infinity
	}
	if df.NaN != "" {
		out.NaN = df.NaN
	}
	if df.MinusSign != "" {
		out.MinusSign = qt3SingleRune(df.MinusSign)
	}
	return out
}

func qt3SingleRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// ──────────────────────────────────────────────────────────────────────
// Runner
// ──────────────────────────────────────────────────────────────────────

func qt3RunTests(t *testing.T, tests []qt3Test) {
	t.Helper()
	// testdata/qt3ts is gitignored; on a fresh checkout without a fetch the
	// fixtures are absent. Skip gracefully instead of failing on a missing
	// document, so `go test ./xpath3/` before a fetch skips rather than fails.
	// This covers every caller, including the hand-written collection /
	// source-document tests that run cases directly.
	if _, err := os.Stat(filepath.Join(qt3TestDataDir(), "catalog.xml")); os.IsNotExist(err) {
		t.Skipf("fixtures not fetched; run go run ./cmd/w3cgen fetch qt3")
	}
	// Register resource mappings into the shared server
	for _, tc := range tests {
		if len(tc.ResourceMap) > 0 {
			qt3RegisterResources(tc.ResourceMap)
		}
	}
	httpClient := qt3GetSharedClient()
	exp := qt3LoadExpectations()
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			if reason, ok := exp.Skip[tc.Name]; ok {
				t.Skip(reason)
			}
			if tc.Skip != "" {
				t.Skip(tc.Skip)
			}
			ctx := t.Context()
			// QT3 test suite expects implicit timezone of -05:00 (PT5H).
			qt3ImplicitTZ := time.FixedZone("", -5*3600)
			opts := []qt3EvalMutator{
				func(e xpath3.Evaluator) xpath3.Evaluator { return e.ImplicitTimezone(qt3ImplicitTZ) },
				func(e xpath3.Evaluator) xpath3.Evaluator { return e.HTTPClient(httpClient) },
				func(e xpath3.Evaluator) xpath3.Evaluator { return e.URIResolver(qt3Resolver(httpClient)) },
				// Override the fn:transform stub with the real xslt3 implementation,
				// wired to the same HTTP+filesystem resolver fn:doc uses so
				// stylesheet-location / source-node resources resolve identically.
				qt3TransformMutator(httpClient, qt3TransformBaseURI(tc)),
			}
			if tc.DefaultDecimal != nil {
				df := tc.DefaultDecimal.toXPath3()
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.DefaultDecimalFormat(df) })
			}
			if len(tc.NamedDecimalFormats) > 0 {
				dfs := make(map[xpath3.QualifiedName]xpath3.DecimalFormat, len(tc.NamedDecimalFormats))
				for _, df := range tc.NamedDecimalFormats {
					dfs[xpath3.QualifiedName{URI: df.URI, Name: df.Name}] = df.Format.toXPath3()
				}
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.NamedDecimalFormats(dfs) })
			}
			if tc.DefaultCollation != "" {
				collation := tc.DefaultCollation
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator {
					return e.DefaultCollation(collation)
				})
			}
			if tc.DefaultLanguage != "" {
				lang := tc.DefaultLanguage
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.DefaultLanguage(lang) })
			}
			if len(tc.Namespaces) > 0 {
				ns := tc.Namespaces
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.Namespaces(ns) })
			}
			if tc.XML11 {
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.AllowXML11Chars() })
			}
			if tc.BaseURI != "" {
				uri := tc.BaseURI
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.BaseURI(uri) })
			} else if baseURI := qt3DefaultBaseURI(tc); baseURI != "" {
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.BaseURI(baseURI) })
			}
			var doc helium.Node
			if tc.DocPath != "" {
				doc = qt3ParseDoc(t, filepath.Join(qt3TestDataDir(), tc.DocPath))
			}
			var vars map[string]xpath3.Sequence
			if len(tc.SourceDocs) > 0 || len(tc.Params) > 0 {
				vars = make(map[string]xpath3.Sequence, len(tc.SourceDocs)+len(tc.Params))
			}
			// Documents whose types must be annotated (context + strict/lax sources).
			var validatedDocs []helium.Node
			if (tc.ContextValidation == "strict" || tc.ContextValidation == "lax") && doc != nil {
				validatedDocs = append(validatedDocs, doc)
			}
			for _, src := range tc.SourceDocs {
				sourceDoc := qt3ParseDocSource(t, src)
				vars[src.Name] = xpath3.ItemSlice{xpath3.NodeItem{Node: sourceDoc}}
				if src.Validation == "strict" || src.Validation == "lax" {
					validatedDocs = append(validatedDocs, sourceDoc)
				}
			}
			if len(tc.Schemas) > 0 {
				opts = append(opts, qt3SchemaOpts(t, ctx, tc, doc, validatedDocs)...)
			}
			if resolver := qt3BuildCollectionResolver(t, ctx, tc, opts, doc, vars); resolver != nil {
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.CollectionResolver(resolver) })
			}
			if len(tc.Params) > 0 {
				for _, param := range tc.Params {
					paramEval := qt3ApplyEval(xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions), opts)
					if len(vars) > 0 {
						paramEval = paramEval.Variables(vars)
					}
					compiledParam, err := xpath3.NewCompiler().Compile(param.Select)
					require.NoError(t, err, "compile param $%s: %s", param.Name, param.Select)
					result, err := paramEval.Evaluate(ctx, compiledParam, doc)
					require.NoError(t, err, "eval param $%s: %s", param.Name, param.Select)
					vars[param.Name] = result.Sequence()
				}
			}
			if len(vars) > 0 {
				v := vars
				opts = append(opts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.Variables(v) })
			}
			eval := qt3ApplyEval(xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions), opts)
			if reason, ok := exp.XFail[tc.Name]; ok {
				qt3RunXFail(t, ctx, tc, eval, doc, reason)
				return
			}
			qt3CheckResult(t, ctx, tc, eval, doc)
		})
	}
}

// qt3CheckResult runs a case's compile/evaluate/assert phase, reporting failures
// through t. t is the subtest's *testing.T on the normal path and a qt3Recorder
// on the xfail path (see qt3RunXFail).
func qt3CheckResult(t qt3TB, ctx context.Context, tc qt3Test, eval xpath3.Evaluator, doc helium.Node) {
	t.Helper()
	compiled, err := xpath3.NewCompiler().Compile(tc.XPath)
	if err != nil {
		if tc.ExpectError || tc.AcceptError {
			return
		}
		require.NoError(t, err, "compile: %s", tc.XPath)
	}
	result, err := eval.Evaluate(ctx, compiled, doc)
	if err != nil {
		if tc.ExpectError || tc.AcceptError {
			return
		}
		require.NoError(t, err, "eval: %s", tc.XPath)
	}
	if tc.ExpectError {
		t.Fatalf("expected error but got result: %v", result.Sequence())
	}
	seq := result.Sequence()
	for _, a := range tc.Assertions {
		a(t, seq)
	}
}

// qt3RunXFail runs an xfail-listed case's result-check phase against a recorder
// in a dedicated goroutine (so a require FailNow / t.Fatalf Goexit stays
// contained instead of ending the subtest), then asserts the case did NOT pass.
// An xfail that now passes is a hard error: the divergence is resolved and the
// entry must be removed from expectations/qt3.json (mirrors the xsd10 harness).
func qt3RunXFail(t *testing.T, ctx context.Context, tc qt3Test, eval xpath3.Evaluator, doc helium.Node, reason string) {
	t.Helper()
	rec := &qt3Recorder{ctx: ctx}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				rec.failed = true
				rec.msgs = append(rec.msgs, fmt.Sprintf("panic: %v", r))
			}
		}()
		qt3CheckResult(rec, ctx, tc, eval, doc)
	}()
	<-done
	if !rec.failed {
		t.Errorf("XFAIL %s unexpectedly PASSED — divergence resolved; remove it from expectations/qt3.json xfail (%s)", tc.Name, reason)
		return
	}
	t.Logf("xfail (%s): %s", reason, strings.Join(rec.msgs, "; "))
}

// ──────────────────────────────────────────────────────────────────────
// Schema-aware wiring
// ──────────────────────────────────────────────────────────────────────

// qt3SchemaOpts compiles the test's in-scope schemas and returns evaluator
// mutators that install the SchemaDeclarations provider and (for strict/lax
// sources) the collected type annotations. A schema that fails to compile
// skips the case (a helium xsd limitation, reported as a skip rather than a
// hard failure). Annotation maps are keyed on the exact parsed document nodes
// the evaluator sees, so schema types atomize/cast as declared.
func qt3SchemaOpts(t *testing.T, ctx context.Context, tc qt3Test, doc helium.Node, validatedDocs []helium.Node) []qt3EvalMutator {
	t.Helper()
	schemas := make([]*xsd.Schema, 0, len(tc.Schemas))
	for _, sc := range tc.Schemas {
		schemaPath := filepath.Join(qt3TestDataDir(), sc.DocPath)
		// Trusted committed W3C fixtures whose nested xs:include/xs:import
		// targets live next to the schema; opt into host FS access. Default to
		// XSD 1.1 (honoring an xsd-version="1.0" dependency), mirroring the
		// production schema-aware paths.
		compiler := xsd.NewCompiler().FS(helium.PermissiveFS())
		if tc.SchemaVersion == "1.0" {
			compiler = compiler.Version(xsd.Version10)
		} else {
			compiler = compiler.DefaultVersion(xsd.Version11)
		}
		schema, err := compiler.CompileFile(ctx, schemaPath)
		if err != nil {
			t.Skipf("schema %q failed to compile: %v", sc.DocPath, err)
		}
		schemas = append(schemas, schema)
	}
	if len(schemas) == 0 {
		return nil
	}

	var muts []qt3EvalMutator
	if d := qt3AggregateSchemaDecls(schemas); d != nil {
		muts = append(muts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.SchemaDeclarations(d) })
	}

	if len(validatedDocs) > 0 {
		ann := make(xsd.TypeAnnotations)
		nilled := make(map[helium.Node]struct{})
		ids := make(map[helium.Node]struct{})
		for _, vd := range validatedDocs {
			qt3AnnotateDoc(t, ctx, schemas, vd, ann, nilled, ids)
		}
		if len(ann) > 0 {
			muts = append(muts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.TypeAnnotations(ann) })
		}
		if len(nilled) > 0 {
			muts = append(muts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.NilledElements(nilled) })
		}
		if len(ids) > 0 {
			muts = append(muts, func(e xpath3.Evaluator) xpath3.Evaluator { return e.IDNodes(ids) })
		}
	}
	return muts
}

// qt3AggregateSchemaDecls builds one SchemaDeclarations provider spanning every
// in-scope schema: a lookup consults each schema's declarations in order and
// returns the first hit, mirroring how a multi-schema registry delegates. This
// lets a schema-element()/schema-attribute() test or a schema-aware cast resolve
// against any in-scope namespace, not just one schema's.
func qt3AggregateSchemaDecls(schemas []*xsd.Schema) xpath3.SchemaDeclarations {
	if len(schemas) == 0 {
		return nil
	}
	decls := make(qt3AggregateDecls, 0, len(schemas))
	for _, s := range schemas {
		decls = append(decls, s.Declarations())
	}
	return decls
}

// qt3AggregateDecls delegates each SchemaDeclarations method across the wrapped
// providers, first-hit for lookups and OR for predicates. A cast/validation
// method returns the first non-nil error (a schema that does not define the type
// reports "not found" as nil, so only the owning schema can report a facet
// violation).
type qt3AggregateDecls []xpath3.SchemaDeclarations

func (a qt3AggregateDecls) LookupSchemaElement(local, ns string) (string, bool) {
	for _, d := range a {
		if t, ok := d.LookupSchemaElement(local, ns); ok {
			return t, true
		}
	}
	return "", false
}

func (a qt3AggregateDecls) LookupSchemaAttribute(local, ns string) (string, bool) {
	for _, d := range a {
		if t, ok := d.LookupSchemaAttribute(local, ns); ok {
			return t, true
		}
	}
	return "", false
}

func (a qt3AggregateDecls) LookupSchemaType(local, ns string) (string, bool) {
	for _, d := range a {
		if t, ok := d.LookupSchemaType(local, ns); ok {
			return t, true
		}
	}
	return "", false
}

func (a qt3AggregateDecls) IsSubtypeOf(typeName, baseTypeName string) bool {
	for _, d := range a {
		if d.IsSubtypeOf(typeName, baseTypeName) {
			return true
		}
	}
	return false
}

func (a qt3AggregateDecls) IsSubstitutionGroupMember(memberLocal, memberNS, headLocal, headNS string) bool {
	for _, d := range a {
		if d.IsSubstitutionGroupMember(memberLocal, memberNS, headLocal, headNS) {
			return true
		}
	}
	return false
}

func (a qt3AggregateDecls) ValidateCast(ctx context.Context, value, typeName string) error {
	for _, d := range a {
		if err := d.ValidateCast(ctx, value, typeName); err != nil {
			return err
		}
	}
	return nil
}

func (a qt3AggregateDecls) ValidateCastWithNS(ctx context.Context, value, typeName string, nsMap map[string]string) error {
	for _, d := range a {
		if err := d.ValidateCastWithNS(ctx, value, typeName, nsMap); err != nil {
			return err
		}
	}
	return nil
}

func (a qt3AggregateDecls) ListItemType(typeName string) (string, bool) {
	for _, d := range a {
		if it, ok := d.ListItemType(typeName); ok {
			return it, true
		}
	}
	return "", false
}

func (a qt3AggregateDecls) UnionMemberTypes(typeName string) []string {
	for _, d := range a {
		if m := d.UnionMemberTypes(typeName); m != nil {
			return m
		}
	}
	return nil
}

// SchemaTypeContentKind satisfies the optional xpath3.ContentTypeKindProvider
// interface so fn:data can raise FOTY0012 for a complex-content element with no
// typed value. It consults each wrapped provider that implements the interface
// and returns the first that reports a hit, mirroring the first-hit lookups.
func (a qt3AggregateDecls) SchemaTypeContentKind(typeName string) (xpath3.ContentTypeKind, bool) {
	for _, d := range a {
		provider, ok := d.(xpath3.ContentTypeKindProvider)
		if !ok {
			continue
		}
		if kind, found := provider.SchemaTypeContentKind(typeName); found {
			return kind, true
		}
	}
	return 0, false
}

// qt3AnnotateDoc validates node against the schema whose target namespace
// matches its root element and merges the resulting type annotations into ann,
// the PSVI nilled elements into nilled, and the PSVI is-id nodes into ids (both
// keyed on the exact document nodes the evaluator sees, so fn:nilled and
// fn:id/fn:element-with-id observe the validated properties). The fixtures are
// expected valid; a strict/lax source that FAILS validation is skipped (with
// schema/doc context) rather than run with incomplete PSVI annotations that
// would silently distort the result.
func qt3AnnotateDoc(t *testing.T, ctx context.Context, schemas []*xsd.Schema, node helium.Node, ann xsd.TypeAnnotations, nilled, ids map[helium.Node]struct{}) {
	t.Helper()
	d := qt3AsDocument(node)
	if d == nil {
		return
	}
	schema := qt3PickValidationSchema(schemas, d)
	if schema == nil {
		return
	}
	local := make(xsd.TypeAnnotations)
	localNilled := make(xsd.NilledElements)
	localIDs := make(xsd.IDNodes)
	validator := xsd.NewValidator(schema).Annotations(&local).NilledElements(&localNilled).IDNodes(&localIDs)
	if err := validator.Validate(ctx, d); err != nil {
		t.Skipf("validating source (root ns %q) against schema %q: %v", qt3DocRootNS(d), schema.TargetNamespace(), err)
	}
	for k, v := range local {
		ann[k] = v
	}
	for e := range localNilled {
		nilled[e] = struct{}{}
	}
	for n := range localIDs {
		ids[n] = struct{}{}
	}
}

// qt3PickValidationSchema returns the schema whose target namespace exactly
// matches d's root element namespace. When there is a single in-scope schema it
// is used as the fallback (it is the only candidate); otherwise a mismatch
// yields nil rather than validating against an arbitrary schema.
func qt3PickValidationSchema(schemas []*xsd.Schema, d *helium.Document) *xsd.Schema {
	if len(schemas) == 0 {
		return nil
	}
	if d != nil {
		ns := qt3DocRootNS(d)
		for _, s := range schemas {
			if s.TargetNamespace() == ns {
				return s
			}
		}
	}
	if len(schemas) == 1 {
		return schemas[0]
	}
	return nil
}

func qt3AsDocument(n helium.Node) *helium.Document {
	if n == nil {
		return nil
	}
	if d, ok := n.(*helium.Document); ok {
		return d
	}
	if owner := n.OwnerDocument(); owner != nil {
		return owner
	}
	return nil
}

func qt3DocRootNS(d *helium.Document) string {
	if d == nil {
		return ""
	}
	root := d.DocumentElement()
	if root == nil {
		return ""
	}
	return root.URI()
}

// ──────────────────────────────────────────────────────────────────────
// Path helpers
// ──────────────────────────────────────────────────────────────────────

func qt3TestDataDir() string {
	return qt3RepoTestDataDir()
}

func qt3DefaultBaseURI(tc qt3Test) string {
	if qt3NeedsParseXMLBaseURI(tc.XPath) {
		return "http://www.w3.org/fots/fn/"
	}
	if qt3NeedsRelativeUnparsedTextBaseURI(tc.XPath) && (tc.NeedsHTTP || len(tc.ResourceMap) > 0) {
		// Relative QT3 unparsed-text fixtures live under testdata/qt3ts/fn/.
		return "http://www.w3.org/fots/fn/"
	}
	if qt3NeedsRelativeParseJSONFixtureBaseURI(tc.XPath) {
		return "http://www.w3.org/fots/fn/"
	}
	if strings.Contains(tc.XPath, "static-base-uri()") {
		return qtFotsCatalogNS
	}
	if baseURI := qt3ResourceMapBaseURI(tc); baseURI != "" {
		return baseURI
	}
	if tc.NeedsHTTP {
		return "http://www.w3.org/fots/"
	}
	return ""
}

func qt3ResourceMapBaseURI(tc qt3Test) string {
	if !tc.NeedsHTTP || len(tc.ResourceMap) == 0 {
		return ""
	}

	baseDir := ""
	for uri, relPath := range tc.ResourceMap {
		if strings.Contains(uri, "://") {
			return ""
		}
		dir := qt3RelativeResourceBase(relPath, uri)
		if dir == "" {
			return ""
		}
		if baseDir == "" {
			baseDir = dir
			continue
		}
		if baseDir != dir {
			return ""
		}
	}

	if baseDir == "" {
		return ""
	}
	return "http://www.w3.org/fots/" + strings.Trim(baseDir, "/") + "/"
}

func qt3RelativeResourceBase(relPath, uri string) string {
	relDir := path.Dir(filepath.ToSlash(relPath))
	uriDir := path.Dir(uri)
	if relDir == "." || relDir == "" {
		return ""
	}
	if uriDir == "." || uriDir == "" {
		return relDir
	}

	relParts := strings.Split(relDir, "/")
	uriParts := strings.Split(uriDir, "/")
	if len(uriParts) > len(relParts) {
		return ""
	}
	for i := 1; i <= len(uriParts); i++ {
		if relParts[len(relParts)-i] != uriParts[len(uriParts)-i] {
			return ""
		}
	}
	baseParts := relParts[:len(relParts)-len(uriParts)]
	if len(baseParts) == 0 {
		return ""
	}
	return strings.Join(baseParts, "/")
}

func qt3NeedsRelativeUnparsedTextBaseURI(expr string) bool {
	const name = "unparsed-text"

	_, rest, found := strings.Cut(expr, name)
	if !found {
		return false
	}
	return strings.HasPrefix(rest, "(") ||
		strings.HasPrefix(rest, "-lines(") ||
		strings.HasPrefix(rest, "-available(")
}

func qt3NeedsRelativeParseJSONFixtureBaseURI(expr string) bool {
	return strings.Contains(expr, "parse-json(unparsed-text('parse-json/") ||
		strings.Contains(expr, `parse-json(unparsed-text("parse-json/`)
}

func qt3NeedsParseXMLBaseURI(expr string) bool {
	return strings.Contains(expr, "parse-xml(") ||
		strings.Contains(expr, "parse-xml-fragment(")
}

func qt3ParseDoc(t *testing.T, p string) helium.Node {
	t.Helper()
	data, err := os.ReadFile(p)
	require.NoError(t, err, "reading %s", p)
	doc, err := helium.NewParser().Parse(t.Context(), data)
	require.NoError(t, err, "parsing %s", p)
	absPath, err := filepath.Abs(p)
	if err == nil {
		doc.SetURL(qt3DocBaseURI(absPath))
	}
	return doc
}

// qt3DocBaseURI turns a native absolute path into the base URI stored on a
// parsed QT3 document. On POSIX the absolute path ("/abs/x.xml") is used as-is,
// preserving the historical behavior. On Windows the path is "D:\\a\\x.xml":
// stored verbatim, url.Parse would read the drive letter "D" as a URI scheme
// and mangle every relative-reference resolution (e.g. an absolute http: ref in
// a resource map would be wrongly filepath-joined). Convert such a path to a
// canonical "file:///D:/a/x.xml" URI so resolution behaves identically on both
// platforms. Detection is string-shaped (uripath), so POSIX output is unchanged.
func qt3DocBaseURI(absPath string) string {
	if qt3IsWindowsAbsolute(absPath) {
		return qt3WindowsToFileURI(absPath)
	}
	return absPath
}

func qt3ParseDocSource(t *testing.T, src qt3SourceDoc) helium.Node {
	t.Helper()
	doc := qt3ParseDoc(t, filepath.Join(qt3TestDataDir(), src.DocPath))
	if src.URI != "" {
		if document, ok := doc.(*helium.Document); ok {
			document.SetURL(src.URI)
		} else if owner := doc.OwnerDocument(); owner != nil {
			owner.SetURL(src.URI)
		}
	}
	return doc
}

func qt3BuildCollectionResolver(t *testing.T, ctx context.Context, tc qt3Test, opts []qt3EvalMutator, doc helium.Node, vars map[string]xpath3.Sequence) xpath3.CollectionResolver {
	t.Helper()
	if len(tc.Collections) == 0 {
		return nil
	}

	resolver := &qt3CollectionResolver{
		collections:    make(map[string]xpath3.Sequence, len(tc.Collections)),
		uriCollections: make(map[string][]string, len(tc.Collections)),
	}

	queryEval := qt3ApplyEval(xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions), opts)
	if len(vars) > 0 {
		queryEval = queryEval.Variables(vars)
	}

	for _, col := range tc.Collections {
		if len(col.SourceDocs) > 0 {
			seq := make(xpath3.ItemSlice, 0, len(col.SourceDocs))
			uris := make([]string, 0, len(col.SourceDocs))
			for _, src := range col.SourceDocs {
				// LIMITATION: collection source docs are parsed without XSD type
				// annotations. The generator does not propagate a collection
				// source's validation="strict"/"lax" (qt3SourceDoc.Validation is
				// unset here), and no schema-validated collection source exists in
				// the FOTS suite (verified), so no runnable case is affected. To
				// support one, thread Validation into qt3Collection.SourceDocs and
				// annotate these exact nodes before building the collection.
				sourceDoc := qt3ParseDocSource(t, src) //nolint:contextcheck
				seq = append(seq, xpath3.NodeItem{Node: sourceDoc})
				if src.URI != "" {
					uris = append(uris, src.URI)
				}
			}
			resolver.collections[col.URI] = seq
			resolver.uriCollections[col.URI] = uris
			continue
		}

		if col.Query == "" {
			resolver.collections[col.URI] = nil
			resolver.uriCollections[col.URI] = nil
			continue
		}

		expr, err := xpath3.NewCompiler().Compile(col.Query)
		require.NoError(t, err, "compile collection %q query: %s", col.URI, col.Query)
		result, err := queryEval.Evaluate(ctx, expr, doc)
		require.NoError(t, err, "eval collection %q query: %s", col.URI, col.Query)
		resolver.collections[col.URI] = result.Sequence()
		resolver.uriCollections[col.URI] = qt3CollectionURIs(t, col.URI, result.Sequence())
	}

	return resolver
}

func qt3CollectionURIs(t *testing.T, uri string, seq xpath3.Sequence) []string {
	t.Helper()
	if seq == nil {
		return nil
	}
	uris := make([]string, 0, seq.Len())
	for item := range seq.Items() {
		av, err := xpath3.AtomizeItem(item)
		require.NoError(t, err, "atomize collection %q member", uri)
		if av.TypeName != xpath3.TypeAnyURI {
			continue
		}
		s, err := xpath3.AtomicToString(av)
		require.NoError(t, err, "stringify collection %q URI member", uri)
		uris = append(uris, s)
	}
	return uris
}

// ──────────────────────────────────────────────────────────────────────
// Value helpers
// ──────────────────────────────────────────────────────────────────────

func qt3StringValue(seq xpath3.Sequence) string {
	if seq == nil {
		return ""
	}
	var parts []string
	for item := range seq.Items() {
		av, err := xpath3.AtomizeItem(item)
		if err != nil {
			parts = append(parts, fmt.Sprintf("%v", item))
		} else {
			s, serr := xpath3.AtomicToString(av)
			if serr != nil {
				parts = append(parts, fmt.Sprintf("%v", av.Value))
			} else {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " ")
}

func qt3EBV(seq xpath3.Sequence) (bool, error) {
	if seq == nil || seq.Len() == 0 {
		return false, nil
	}
	first := seq.Get(0)
	if _, ok := first.(xpath3.NodeItem); ok {
		return true, nil
	}
	if seq.Len() == 1 {
		av, err := xpath3.AtomizeItem(first)
		if err != nil {
			return false, err
		}
		switch v := av.Value.(type) {
		case bool:
			return v, nil
		case string:
			return v != "", nil
		case *xpath3.FloatValue:
			f := v.Float64()
			return f != 0 && !math.IsNaN(f), nil
		case int64:
			return v != 0, nil
		case *big.Int:
			return v.Sign() != 0, nil
		case *big.Rat:
			return v.Sign() != 0, nil
		}
	}
	return false, fmt.Errorf("cannot compute EBV for sequence of length %d", seq.Len())
}

// ──────────────────────────────────────────────────────────────────────
// Assertion factories  (for direct use in Assertions slice)
// ──────────────────────────────────────────────────────────────────────

func qt3AssertEq(expected string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		// assert-eq: expected is an XPath expression; evaluate it and compare using eq operator
		compiled, err := xpath3.NewCompiler().Compile(expected)
		if err != nil {
			// Not a valid XPath expr — compare as literal string
			require.Equal(t, expected, qt3StringValue(seq))
			return
		}
		result, err := xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions).Evaluate(t.Context(), compiled, nil)
		if err != nil {
			require.Equal(t, expected, qt3StringValue(seq))
			return
		}
		// Try value comparison using eq operator for singleton atomic values
		expSeq := result.Sequence()
		if seq.Len() == 1 && expSeq.Len() == 1 {
			av, aErr := xpath3.AtomizeItem(seq.Get(0))
			bv, bErr := xpath3.AtomizeItem(expSeq.Get(0))
			if aErr == nil && bErr == nil {
				eq, cmpErr := xpath3.ValueCompare(xpath3.TokenEq, av, bv)
				if cmpErr == nil {
					if eq {
						return // values are equal via eq
					}
					// Fall through to string comparison for better error message
				}
			}
		}
		require.Equal(t, qt3StringValue(expSeq), qt3StringValue(seq))
	}
}

func qt3AssertStringValue(expected string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		require.Equal(t, expected, qt3StringValue(seq))
	}
}

func qt3AssertStringValueNS(expected string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		expected = strings.Join(strings.Fields(expected), " ")
		got := strings.Join(strings.Fields(qt3StringValue(seq)), " ")
		require.Equal(t, expected, got)
	}
}

func qt3AssertTrue() qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		ebv, err := qt3EBV(seq)
		require.NoError(t, err)
		require.True(t, ebv, "expected true, got: %v", qt3StringValue(seq))
	}
}

func qt3AssertFalse() qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		ebv, err := qt3EBV(seq)
		require.NoError(t, err)
		require.False(t, ebv, "expected false, got: %v", qt3StringValue(seq))
	}
}

func qt3AssertEmpty() qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		require.True(t, seq == nil || seq.Len() == 0, "expected empty sequence")
	}
}

func qt3AssertCount(n int) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		if seq == nil {
			require.Equal(t, 0, n)
		} else {
			require.Equal(t, n, seq.Len())
		}
	}
}

// qt3InstanceOfExpr builds the "$result instance of T" XPath used to check a
// FOTS assert-type. T is an XPath 3.1 SequenceType and is spliced verbatim; it
// must NOT be parenthesized — helium's grammar rejects a parenthesized item type
// with an occurrence indicator (e.g. "( xs:integer+ )"), which is exactly the
// grammar of a SequenceType.
func qt3InstanceOfExpr(typ string) string {
	return "$result instance of " + typ
}

// qt3AssertType evaluates the FOTS <assert-type>T</assert-type> assertion:
// $result must match the XPath 3.1 SequenceType T. It compiles and evaluates
// "$result instance of T" with $result bound to the case result and the case's
// namespaces (reusing the generic-<assert> plumbing, qt3EvalAssert), and requires
// an effective boolean value of true. A SequenceType helium's xpath3 cannot
// compile surfaces as a compile error — never a silent pass; such a case is
// xfailed in expectations/qt3.json with a specific reason.
func qt3AssertType(typ string, ns map[string]string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		ok, err := qt3EvalAssert(qt3InstanceOfExpr(typ), ns, seq)
		require.NoError(t, err, "assert-type %s", typ)
		require.True(t, ok, "expected result to match type %s, got %s", typ, qt3StringValue(seq))
	}
}

func qt3AssertDeepEq(expected string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		compiled, err := xpath3.NewCompiler().Compile(expected)
		if err != nil {
			// Fall back to string comparison if the expected value doesn't compile
			require.Equal(t, expected, qt3StringValue(seq), "deep-eq")
			return
		}
		result, err := xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions).Evaluate(t.Context(), compiled, nil)
		if err != nil {
			require.Equal(t, expected, qt3StringValue(seq), "deep-eq")
			return
		}
		expectedSeq := result.Sequence()
		if !qt3DeepEqualSeq(seq, expectedSeq) {
			require.Equal(t, qt3FormatSeq(expectedSeq), qt3FormatSeq(seq), "deep-eq")
		}
	}
}

// qt3AssertSkip is a no-op assertion for unimplemented assertion types.
func qt3AssertSkip() qt3Assertion {
	return func(_ qt3TB, _ xpath3.Sequence) {}
}

// qt3EvalAssert compiles a generic FOTS <assert> expression, evaluates it with
// $result bound to the case's result sequence and the case's in-scope namespaces
// (ns; the XPath 3.1 predeclared prefixes fn/math/map/array/err/xs come from the
// evaluator's static context), and returns its effective boolean value. A
// compile error, evaluation error, or an EBV that raises (e.g. FORG0006 on a
// multi-item non-node sequence) is returned as the error — the assertion fails.
func qt3EvalAssert(expr string, ns map[string]string, seq xpath3.Sequence) (bool, error) {
	got, err := qt3EvalExprSeq(expr, ns, seq)
	if err != nil {
		return false, err
	}
	return qt3EBV(got)
}

// qt3EvalExprSeq compiles expr, evaluates it with $result bound to seq and the
// case's in-scope namespaces (ns), and returns the result sequence. It is the
// shared plumbing behind the generic <assert>, assert-type, assert-xml (via
// fn:serialize) and assert-permutation checks.
func qt3EvalExprSeq(expr string, ns map[string]string, seq xpath3.Sequence) (xpath3.Sequence, error) {
	compiled, err := xpath3.NewCompiler().Compile(expr)
	if err != nil {
		return nil, err
	}
	eval := xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions)
	if len(ns) > 0 {
		eval = eval.Namespaces(ns)
	}
	if seq == nil {
		seq = xpath3.EmptySequence()
	}
	eval = eval.Variables(map[string]xpath3.Sequence{"result": seq})
	result, err := eval.Evaluate(context.Background(), compiled, nil)
	if err != nil {
		return nil, err
	}
	return result.Sequence(), nil
}

// qt3Assert evaluates a generic FOTS <assert>EXPR</assert>: EXPR is a boolean
// XPath over $result and must have an effective boolean value of true.
func qt3Assert(expr string, ns map[string]string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		ebv, err := qt3EvalAssert(expr, ns, seq)
		require.NoError(t, err, "assert: %s", expr)
		require.True(t, ebv, "assert: %s (got %s)", expr, qt3StringValue(seq))
	}
}

// ──────────────────────────────────────────────────────────────────────
// assert-xml: serialize $result and compare against the expected fragment
// ──────────────────────────────────────────────────────────────────────

const (
	qt3AssertXMLWrapOpen  = "<qt3-assert-xml-wrap>"
	qt3AssertXMLWrapClose = "</qt3-assert-xml-wrap>"
)

// qt3SerializeResult serializes $result as XML (method="xml",
// omit-xml-declaration="yes") via fn:serialize, exactly the serialization FOTS
// assert-xml compares against. A serialization error (e.g. SENR0001 for a bare
// attribute/namespace node) is returned rather than silently swallowed.
func qt3SerializeResult(seq xpath3.Sequence, ns map[string]string) (string, error) {
	const expr = `serialize($result, map{'method':'xml','omit-xml-declaration':true()})`
	out, err := qt3EvalExprSeq(expr, ns, seq)
	if err != nil {
		return "", err
	}
	return qt3StringValue(out), nil
}

// qt3AssertXML evaluates FOTS <assert-xml>: $result serialized as XML must equal
// the expected fragment. The comparison is canonical (attribute-order- and
// namespace-prefix-insensitive, both infoset-insignificant) so a correct result
// is not flagged over serialization order; content, structure, names and values
// must match. A serialization failure is a hard assertion failure, never a
// silent pass.
func qt3AssertXML(expected string, normalizeSpace bool, ns map[string]string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		got, err := qt3SerializeResult(seq, ns)
		require.NoError(t, err, "assert-xml: serialize $result")
		require.True(t, qt3XMLResultMatches(got, expected, normalizeSpace),
			"assert-xml mismatch\n want: %s\n  got: %s", expected, got)
	}
}

// qt3CheckXML is the any-of form of qt3AssertXML.
func qt3CheckXML(expected string, normalizeSpace bool, ns map[string]string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		got, err := qt3SerializeResult(seq, ns)
		if err != nil {
			return false
		}
		return qt3XMLResultMatches(got, expected, normalizeSpace)
	}
}

func qt3XMLResultMatches(got, expected string, normalizeSpace bool) bool {
	if normalizeSpace {
		return qt3CollapseWS(got) == qt3CollapseWS(expected)
	}
	if got == expected {
		return true
	}
	return qt3CanonicalXMLEqual(got, expected)
}

func qt3CollapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// qt3CanonicalXMLEqual reports whether two XML fragments are equal ignoring
// attribute order and namespace-prefix choice. Each fragment is wrapped in a
// synthetic root, parsed, and reduced to a canonical node tree compared with
// reflect.DeepEqual. A fragment that fails to parse compares unequal (so the
// assertion fails and is triaged, never silently passes).
func qt3CanonicalXMLEqual(got, expected string) bool {
	gotNodes, err := qt3ParseFragmentChildren(got)
	if err != nil {
		return false
	}
	expNodes, err := qt3ParseFragmentChildren(expected)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(qt3CanonList(gotNodes), qt3CanonList(expNodes))
}

func qt3ParseFragmentChildren(frag string) ([]helium.Node, error) {
	document, err := helium.NewParser().Parse(context.Background(), []byte(qt3AssertXMLWrapOpen+frag+qt3AssertXMLWrapClose))
	if err != nil {
		return nil, err
	}
	root := document.DocumentElement()
	if root == nil {
		return nil, fmt.Errorf("parsed fragment has no root element")
	}
	return qt3ChildNodes(root), nil
}

func qt3ChildNodes(n helium.Node) []helium.Node {
	var out []helium.Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, c)
	}
	return out
}

const (
	qt3CanonText = iota
	qt3CanonComment
	qt3CanonPI
	qt3CanonElem
)

// qt3CanonNode is the order-normalized shape of a node used for canonical XML
// comparison: element identity is (uri, local) with attributes as an unordered
// (uri, local)->value map, so namespace-prefix and attribute-order differences
// are absorbed; text/comment/PI carry their content verbatim.
type qt3CanonNode struct {
	Kind     int
	Text     string
	URI      string
	Local    string
	Attrs    map[[2]string]string
	Children []qt3CanonNode
}

// qt3CanonList reduces a node list to canonical nodes, merging consecutive
// text/CDATA/entity-ref runs into one text node (so a CDATA-vs-text or split-text
// serialization difference does not spuriously mismatch).
func qt3CanonList(nodes []helium.Node) []qt3CanonNode {
	var out []qt3CanonNode
	var textBuf strings.Builder
	hasText := false
	flush := func() {
		if hasText {
			out = append(out, qt3CanonNode{Kind: qt3CanonText, Text: textBuf.String()})
			textBuf.Reset()
			hasText = false
		}
	}
	for _, n := range nodes {
		switch n.Type() {
		case helium.TextNode, helium.CDATASectionNode, helium.EntityRefNode:
			textBuf.Write(n.Content())
			hasText = true
		case helium.CommentNode:
			flush()
			out = append(out, qt3CanonNode{Kind: qt3CanonComment, Text: string(n.Content())})
		case helium.ProcessingInstructionNode:
			flush()
			out = append(out, qt3CanonNode{Kind: qt3CanonPI, Local: n.Name(), Text: string(n.Content())})
		case helium.ElementNode:
			flush()
			out = append(out, qt3CanonElement(n))
		}
	}
	flush()
	return out
}

func qt3CanonElement(n helium.Node) qt3CanonNode {
	attrs := map[[2]string]string{}
	uri, local := "", n.Name()
	if elem, ok := n.(*helium.Element); ok {
		uri, local = elem.URI(), elem.LocalName()
		for _, a := range elem.Attributes() {
			attrs[[2]string{a.URI(), a.LocalName()}] = a.Value()
		}
	}
	return qt3CanonNode{
		Kind:     qt3CanonElem,
		URI:      uri,
		Local:    local,
		Attrs:    attrs,
		Children: qt3CanonList(qt3ChildNodes(n)),
	}
}

// ──────────────────────────────────────────────────────────────────────
// assert-permutation: $result is an unordered multiset match of EXPR
// ──────────────────────────────────────────────────────────────────────

// qt3AssertPermutation evaluates FOTS <assert-permutation>EXPR</assert-permutation>:
// $result and the sequence EXPR produces must contain the same items (deep-equal,
// per item) in any order. A compile/eval failure of EXPR is a hard failure.
func qt3AssertPermutation(expr string, ns map[string]string) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		expected, err := qt3EvalExprSeq(expr, ns, seq)
		require.NoError(t, err, "assert-permutation: eval %s", expr)
		require.True(t, qt3IsPermutation(seq, expected),
			"assert-permutation: %s\n want (any order): %s\n  got: %s", expr, qt3FormatSeq(expected), qt3FormatSeq(seq))
	}
}

// qt3CheckPermutation is the any-of form of qt3AssertPermutation.
func qt3CheckPermutation(expr string, ns map[string]string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		expected, err := qt3EvalExprSeq(expr, ns, seq)
		if err != nil {
			return false
		}
		return qt3IsPermutation(seq, expected)
	}
}

// qt3IsPermutation reports whether got and want are the same multiset of items
// under qt3DeepEqualItem (order-independent). It greedily matches each got item
// to an as-yet-unmatched want item.
func qt3IsPermutation(got, want xpath3.Sequence) bool {
	if qt3SeqLen(got) != qt3SeqLen(want) {
		return false
	}
	n := qt3SeqLen(got)
	used := make([]bool, n)
	for i := 0; i < n; i++ {
		gi := got.Get(i)
		matched := false
		for j := 0; j < n; j++ {
			if used[j] {
				continue
			}
			if qt3DeepEqualItem(gi, want.Get(j)) {
				used[j] = true
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// ──────────────────────────────────────────────────────────────────────
// Check factories  (for use inside qt3AnyOf)
// ──────────────────────────────────────────────────────────────────────

func qt3CheckEq(expected string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		compiled, err := xpath3.NewCompiler().Compile(expected)
		if err != nil {
			return qt3StringValue(seq) == expected
		}
		result, err := xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions).Evaluate(context.Background(), compiled, nil)
		if err != nil {
			return qt3StringValue(seq) == expected
		}
		return qt3StringValue(seq) == qt3StringValue(result.Sequence())
	}
}

func qt3CheckStringValue(expected string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		return qt3StringValue(seq) == expected
	}
}

func qt3CheckStringValueNS(expected string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		got := strings.Join(strings.Fields(qt3StringValue(seq)), " ")
		want := strings.Join(strings.Fields(expected), " ")
		return got == want
	}
}

func qt3CheckTrue() qt3Check {
	return func(seq xpath3.Sequence) bool {
		ebv, err := qt3EBV(seq)
		return err == nil && ebv
	}
}

func qt3CheckFalse() qt3Check {
	return func(seq xpath3.Sequence) bool {
		ebv, err := qt3EBV(seq)
		return err == nil && !ebv
	}
}

func qt3CheckEmpty() qt3Check {
	return func(seq xpath3.Sequence) bool {
		return seq == nil || seq.Len() == 0
	}
}

// qt3CheckType is the any-of form of qt3AssertType: it returns true when
// $result matches the SequenceType T.
func qt3CheckType(typ string, ns map[string]string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		ok, err := qt3EvalAssert(qt3InstanceOfExpr(typ), ns, seq)
		return err == nil && ok
	}
}

func qt3CheckCount(n int) qt3Check {
	return func(seq xpath3.Sequence) bool {
		if seq == nil {
			return n == 0
		}
		return seq.Len() == n
	}
}

func qt3CheckDeepEq(expected string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		compiled, err := xpath3.NewCompiler().Compile(expected)
		if err != nil {
			return qt3StringValue(seq) == expected
		}
		result, err := xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions).Evaluate(context.Background(), compiled, nil)
		if err != nil {
			return qt3StringValue(seq) == expected
		}
		return qt3DeepEqualSeq(seq, result.Sequence())
	}
}

func qt3CheckSkip() qt3Check {
	return func(_ xpath3.Sequence) bool {
		return true
	}
}

// qt3CheckAssert is the any-of form of qt3Assert: it returns true when the
// generic <assert> expression's effective boolean value is true.
func qt3CheckAssert(expr string, ns map[string]string) qt3Check {
	return func(seq xpath3.Sequence) bool {
		ebv, err := qt3EvalAssert(expr, ns, seq)
		return err == nil && ebv
	}
}

// qt3AnyOf passes if any check succeeds.
func qt3AnyOf(checks ...qt3Check) qt3Assertion {
	return func(t qt3TB, seq xpath3.Sequence) {
		t.Helper()
		for _, c := range checks {
			if c(seq) {
				return
			}
		}
		require.Fail(t, "none of the any-of assertions passed", "got: %s", qt3StringValue(seq))
	}
}

// ──────────────────────────────────────────────────────────────────────
// Deep-equal helpers for structural comparison (arrays, maps, atomics)
// ──────────────────────────────────────────────────────────────────────

// qt3DeepEqualSeq compares two sequences structurally.
func qt3SeqLen(s xpath3.Sequence) int {
	if s == nil {
		return 0
	}
	return s.Len()
}

func qt3DeepEqualSeq(a, b xpath3.Sequence) bool {
	if qt3SeqLen(a) != qt3SeqLen(b) {
		return false
	}
	for i := range qt3SeqLen(a) {
		if !qt3DeepEqualItem(a.Get(i), b.Get(i)) {
			return false
		}
	}
	return true
}

func qt3DeepEqualItem(a, b xpath3.Item) bool {
	switch av := a.(type) {
	case xpath3.AtomicValue:
		bv, ok := b.(xpath3.AtomicValue)
		if !ok {
			return false
		}
		return qt3DeepEqualAtomic(av, bv)
	case xpath3.ArrayItem:
		bArr, ok := b.(xpath3.ArrayItem)
		if !ok {
			return false
		}
		if av.Size() != bArr.Size() {
			return false
		}
		for i := 1; i <= av.Size(); i++ {
			am, _ := av.Get(i)
			bm, _ := bArr.Get(i)
			if !qt3DeepEqualSeq(am, bm) {
				return false
			}
		}
		return true
	case xpath3.MapItem:
		bMap, ok := b.(xpath3.MapItem)
		if !ok {
			return false
		}
		if av.Size() != bMap.Size() {
			return false
		}
		keys := av.Keys()
		for _, k := range keys {
			aVal, _ := av.Get(k)
			bVal, found := bMap.Get(k)
			if !found {
				return false
			}
			if !qt3DeepEqualSeq(aVal, bVal) {
				return false
			}
		}
		return true
	case xpath3.NodeItem:
		bn, ok := b.(xpath3.NodeItem)
		if !ok {
			return false
		}
		// Compare node string values
		aStr, _ := xpath3.AtomizeItem(av)
		bStr, _ := xpath3.AtomizeItem(bn)
		return qt3DeepEqualAtomic(aStr, bStr)
	default:
		return false
	}
}

func qt3DeepEqualAtomic(a, b xpath3.AtomicValue) bool {
	// Numeric comparison. deep-equal (and assert-permutation) compare two numerics
	// with the eq operator after type promotion (xs:float(1.01) eq xs:decimal(1.01)
	// promotes the decimal to xs:float, so they are equal) — use helium'"'"'s own
	// ValueCompare so single/double precision promotion matches the spec, not a
	// blanket float64 widening. NaN is special-cased equal (unlike the eq operator).
	if a.IsNumeric() && b.IsNumeric() {
		af, bf := a.ToFloat64(), b.ToFloat64()
		if math.IsNaN(af) || math.IsNaN(bf) {
			return math.IsNaN(af) && math.IsNaN(bf)
		}
		if eq, err := xpath3.ValueCompare(xpath3.TokenEq, a, b); err == nil {
			return eq
		}
		return af == bf
	}
	// String-based comparison for same types
	sa, err1 := xpath3.AtomicToString(a)
	sb, err2 := xpath3.AtomicToString(b)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("%v", a.Value) == fmt.Sprintf("%v", b.Value)
	}
	return sa == sb
}

// qt3FormatSeq returns a human-readable representation of a sequence for error messages.
func qt3FormatSeq(seq xpath3.Sequence) string {
	if seq == nil || seq.Len() == 0 {
		return "()"
	}
	parts := make([]string, seq.Len())
	for i := range seq.Len() {
		parts[i] = qt3FormatItem(seq.Get(i))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, ", ")
}

// qt3SharedServer is a package-level shared HTTP test server.
// All qt3RunTests calls share this single server instead of creating 356 separate ones.
var qt3SharedServer struct {
	once    sync.Once
	srv     *httptest.Server
	client  *http.Client
	pathMap sync.Map // map[string]string: URL path → local file path
	fotsDir string
}

func qt3InitSharedServer() {
	qt3SharedServer.once.Do(func() {
		qt3SharedServer.fotsDir = qt3TestDataDir()
		qt3SharedServer.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check resource map first
			if filePath, ok := qt3SharedServer.pathMap.Load(r.URL.Path); ok {
				http.ServeFile(w, r, filePath.(string))
				return
			}
			// Fallback: /fots/ prefix for unparsed-text resources
			if strings.HasPrefix(r.URL.Path, "/fots/") {
				http.StripPrefix("/fots/", http.FileServer(http.Dir(qt3SharedServer.fotsDir))).ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
		}))
		qt3SharedServer.client = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, qt3SharedServer.srv.Listener.Addr().String())
				},
			},
		}
	})
}

// qt3RegisterResources registers resource URI→file mappings into the shared server.
func qt3RegisterResources(resourceMap map[string]string) {
	dataDir := qt3TestDataDir()
	for uri, relPath := range resourceMap {
		if _, afterScheme, ok := strings.Cut(uri, "://"); ok {
			if slashIdx := strings.Index(afterScheme, "/"); slashIdx >= 0 {
				qt3SharedServer.pathMap.Store(afterScheme[slashIdx:], filepath.Join(dataDir, relPath))
			}
		}
	}
}

// qt3GetSharedClient returns the HTTP client for the shared test server.
func qt3GetSharedClient() *http.Client {
	qt3InitSharedServer()
	return qt3SharedServer.client
}

func qt3FormatItem(item xpath3.Item) string {
	switch v := item.(type) {
	case xpath3.AtomicValue:
		s, err := xpath3.AtomicToString(v)
		if err != nil {
			return fmt.Sprintf("%v", v.Value)
		}
		if v.TypeName == xpath3.TypeString {
			return fmt.Sprintf("%q", s)
		}
		return s
	case xpath3.ArrayItem:
		parts := make([]string, v.Size())
		for i := 1; i <= v.Size(); i++ {
			m, _ := v.Get(i)
			parts[i-1] = qt3FormatSeq(m)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case xpath3.MapItem:
		var parts []string
		keys := v.Keys()
		for _, k := range keys {
			val, _ := v.Get(k)
			ks, _ := xpath3.AtomicToString(k)
			parts = append(parts, fmt.Sprintf("%s: %s", ks, qt3FormatSeq(val)))
		}
		return "map{" + strings.Join(parts, ", ") + "}"
	case xpath3.NodeItem:
		a, err := xpath3.AtomizeItem(v)
		if err != nil {
			return "<node>"
		}
		s, _ := xpath3.AtomicToString(a)
		return s
	default:
		return fmt.Sprintf("%v", item)
	}
}
