package xml

import (
	"bytes"
	stdxml "encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

func bytesReader(data []byte) io.Reader { return bytes.NewReader(data) }

// rawCharset is a stdxml CharsetReader that passes bytes through unchanged, so a
// declared encoding on an ASCII/UTF-8 catalog never fails for lack of a
// converter.
func rawCharset(_ string, input io.Reader) (io.Reader, error) { return input, nil }

// The W3C XML Conformance Test Suite (xmlconf) catalog layout, restricted to the
// elements this generator consumes.
//
// The suite root holds xmlconf.xml, a <TESTSUITE> whose <TESTCASES xml:base="…">
// blocks each pull in a per-collection catalog file (xmltest, sun, ibm, oasis,
// japanese, eduni) through an external general entity declared in the DOCTYPE
// internal subset. Go's encoding/xml does not expand external entities, so the
// top catalog is walked with the entity→systemID map parsed from its DOCTYPE.
//
// Each per-collection catalog is self-contained UTF-8 (no external entities): a
// root <TESTCASES> holding <TEST> elements and, in a few cases, nested
// <TESTCASES> with their own xml:base. A <TEST>'s @URI (and optional @OUTPUT)
// resolve against the accumulated xml:base chain to a suite-root-relative path.

// collectionRef pairs a per-collection catalog file (as declared by an external
// entity in the top catalog) with the xml:base its <TESTCASES> block carries.
// Base is the directory that the collection's TEST @URI values resolve against;
// CatalogFile is the catalog document to parse, both suite-root-relative.
type collectionRef struct {
	Base        string
	CatalogFile string
}

// catTestcases models a <TESTCASES> element: an optional xml:base plus any TEST
// children and nested TESTCASES.
type catTestcases struct {
	XMLBase string         `xml:"base,attr"`
	Tests   []catTest      `xml:"TEST"`
	Nested  []catTestcases `xml:"TESTCASES"`
}

// catTest models a <TEST> element with every attribute the runner and gating
// consult.
type catTest struct {
	ID             string `xml:"ID,attr"`
	Type           string `xml:"TYPE,attr"`
	Entities       string `xml:"ENTITIES,attr"`
	Sections       string `xml:"SECTIONS,attr"`
	URI            string `xml:"URI,attr"`
	Output         string `xml:"OUTPUT,attr"`
	Recommendation string `xml:"RECOMMENDATION,attr"`
	Version        string `xml:"VERSION,attr"`
	Edition        string `xml:"EDITION,attr"`
	Namespace      string `xml:"NAMESPACE,attr"`
}

// genCase is the emitted per-test model: identity, expectation metadata, and the
// suite-root-relative fixture path. No fixture bytes are embedded.
type genCase struct {
	ID             string
	Type           string
	URI            string
	Output         string
	Entities       string
	Recommendation string
	Version        string
	Edition        string
	Namespace      string
	// FixtureRels is the union of every fixture this case references (the test
	// document, its OUTPUT, and the transitive closure of external DTD/entity
	// SYSTEM references), as clean suite-root-relative slash paths. Used by Fetch
	// to copy fixtures; not emitted into the generated test files.
	FixtureRels []string
}

var (
	entityDeclRe = regexp.MustCompile(`(?s)<!ENTITY\s+(\S+)\s+SYSTEM\s+"([^"]+)"\s*>`)
	systemRefRe  = regexp.MustCompile(`SYSTEM\s+(?:"([^"]+)"|'([^']+)')`)
	publicRefRe  = regexp.MustCompile(`PUBLIC\s+(?:"[^"]*"|'[^']*')\s+(?:"([^"]+)"|'([^']+)')`)
	// testcasesBlockRe captures each top-catalog <TESTCASES …>…</TESTCASES>
	// block's attributes (group 1) and body (group 2). The blocks do not nest in
	// xmlconf.xml, so a non-greedy body match is exact.
	testcasesBlockRe = regexp.MustCompile(`(?s)<TESTCASES\b([^>]*)>(.*?)</TESTCASES>`)
	xmlBaseRe        = regexp.MustCompile(`xml:base\s*=\s*"([^"]+)"`)
	entityRefRe      = regexp.MustCompile(`&([A-Za-z0-9._-]+);`)
	fixtureScanExt   = map[string]bool{".xml": true, ".dtd": true, ".ent": true}
)

// readCases parses the xmlconf catalog rooted at xmlconfRoot (the directory
// containing xmlconf.xml) and returns the cases grouped by collection (xmltest,
// sun, ibm, oasis, japanese, eduni). Collection keys are sorted. testsOut is the
// count for the manifest.
func readCases(xmlconfRoot string) (byCollection map[string][]genCase, collections []string, testsOut int, err error) {
	refs, err := parseTopCatalog(xmlconfRoot)
	if err != nil {
		return nil, nil, 0, err
	}

	byCollection = map[string][]genCase{}
	seen := map[string]bool{}
	for _, ref := range refs {
		catPath, perr := generator.ContainedPath(xmlconfRoot, ref.CatalogFile)
		if perr != nil {
			return nil, nil, 0, fmt.Errorf("resolve catalog %q: %w", ref.CatalogFile, perr)
		}
		data, rerr := os.ReadFile(catPath)
		if rerr != nil {
			// The top catalog was present and parsed, so a referenced collection
			// catalog that cannot be read is a corrupt/incomplete source, not a
			// legitimately-absent checkout (that case is handled by the caller
			// stat-ing the source root before calling readCases). Fail loudly so a
			// tampered source cannot silently yield a smaller passing suite.
			return nil, nil, 0, fmt.Errorf("read referenced catalog %s: %w", ref.CatalogFile, rerr)
		}

		collection := collectionKey(ref.CatalogFile)
		cases, perr := parseCollectionCatalog(xmlconfRoot, ref.Base, path.Dir(ref.CatalogFile), data, seen)
		if perr != nil {
			return nil, nil, 0, fmt.Errorf("parse catalog %s: %w", ref.CatalogFile, perr)
		}
		byCollection[collection] = append(byCollection[collection], cases...)
		testsOut += len(cases)
	}

	for name := range byCollection {
		collections = append(collections, name)
	}
	sort.Strings(collections)
	return byCollection, collections, testsOut, nil
}

// collectionKey maps a catalog file's first path segment to a collection key
// (xmltest/xmltest.xml → xmltest, ibm/xml-1.1/ibm_valid.xml → ibm).
func collectionKey(catalogFile string) string {
	seg := catalogFile
	if i := strings.IndexByte(seg, '/'); i >= 0 {
		seg = seg[:i]
	}
	if seg == "" {
		return "misc"
	}
	return seg
}

// parseCollectionCatalog extracts every TEST from a per-collection catalog. It
// token-scans for top-level TEST and TESTCASES elements so it handles both a
// rooted <TESTCASES> catalog (xmltest, oasis, ibm, eduni) and a bare fragment of
// sibling <TEST> elements with no wrapping root (the sun catalogs, which are
// designed to be entity-expanded into the top catalog's <TESTCASES>). base is
// the xml:base inherited from the top catalog.
func parseCollectionCatalog(xmlconfRoot, base, catalogDir string, data []byte, seen map[string]bool) ([]genCase, error) {
	dec := stdxml.NewDecoder(bytesReader(data))
	dec.Strict = false
	// Catalog files are ASCII/UTF-8; treat any declared encoding as raw bytes so
	// the decoder never fails for lack of a charset converter.
	dec.CharsetReader = rawCharset

	var out []genCase
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tokenize catalog: %w", err)
		}
		se, ok := tok.(stdxml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "TESTCASES":
			var tc catTestcases
			if err := dec.DecodeElement(&tc, &se); err != nil {
				return nil, err
			}
			if err := collectCases(xmlconfRoot, base, catalogDir, tc, seen, &out); err != nil {
				return nil, err
			}
		case "TEST":
			var t catTest
			if err := dec.DecodeElement(&t, &se); err != nil {
				return nil, err
			}
			if err := appendTest(xmlconfRoot, base, catalogDir, t, seen, &out); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// collectCases walks a TESTCASES subtree, accumulating the xml:base chain and
// appending a genCase for every TEST that carries a URI.
func collectCases(xmlconfRoot, base, catalogDir string, tc catTestcases, seen map[string]bool, out *[]genCase) error {
	if tc.XMLBase != "" {
		base = joinBase(base, tc.XMLBase)
	}
	for _, t := range tc.Tests {
		if err := appendTest(xmlconfRoot, base, catalogDir, t, seen, out); err != nil {
			return err
		}
	}
	for _, nested := range tc.Nested {
		if err := collectCases(xmlconfRoot, base, catalogDir, nested, seen, out); err != nil {
			return err
		}
	}
	return nil
}

// appendTest resolves and appends a single TEST, skipping ones without a URI/ID
// and de-duplicating by ID (a handful of IDs recur across catalogs). A TEST whose
// resolved URI escapes the suite root is a corrupt/tampered catalog and is a hard
// error, not a silent drop, so a smaller passing suite cannot slip through.
func appendTest(xmlconfRoot, base, catalogDir string, t catTest, seen map[string]bool, out *[]genCase) error {
	if t.URI == "" || t.ID == "" {
		return nil
	}
	if seen[t.ID] {
		return nil
	}
	uriRel := resolveFixture(xmlconfRoot, base, catalogDir, t.URI)
	if _, err := generator.ContainedPath(xmlconfRoot, uriRel); err != nil {
		return fmt.Errorf("test %s: URI %q escapes suite root: %w", t.ID, t.URI, err)
	}
	seen[t.ID] = true
	gc, err := buildCase(xmlconfRoot, uriRel, base, catalogDir, t)
	if err != nil {
		return err
	}
	*out = append(*out, gc)
	return nil
}

// buildCase assembles a genCase from an already-resolved, root-contained uriRel
// and gathers the external-reference closure of its document.
func buildCase(xmlconfRoot, uriRel, base, catalogDir string, t catTest) (genCase, error) {
	gc := genCase{
		ID:             t.ID,
		Type:           t.Type,
		URI:            uriRel,
		Entities:       t.Entities,
		Recommendation: t.Recommendation,
		Version:        t.Version,
		Edition:        t.Edition,
		Namespace:      t.Namespace,
	}
	if t.Output != "" {
		out := resolveFixture(xmlconfRoot, base, catalogDir, t.Output)
		if _, err := generator.ContainedPath(xmlconfRoot, out); err != nil {
			return genCase{}, fmt.Errorf("test %s: OUTPUT %q escapes suite root: %w", t.ID, t.Output, err)
		}
		gc.Output = out
	}

	fixtures := map[string]bool{}
	if gc.Output != "" {
		fixtures[gc.Output] = true
	}
	collectClosure(xmlconfRoot, uriRel, fixtures)

	gc.FixtureRels = make([]string, 0, len(fixtures))
	for rel := range fixtures {
		gc.FixtureRels = append(gc.FixtureRels, rel)
	}
	sort.Strings(gc.FixtureRels)
	return gc, nil
}

// collectClosure BFS-walks the external DTD/entity SYSTEM (and PUBLIC system-id)
// references reachable from the test document, adding every existing file to
// fixtures. Missing references are ignored — they surface as skips at run time.
func collectClosure(xmlconfRoot, startRel string, fixtures map[string]bool) {
	queue := []string{startRel}
	enqueued := map[string]bool{startRel: true}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		fpath, perr := generator.ContainedPath(xmlconfRoot, rel)
		if perr != nil {
			continue
		}
		data, rerr := os.ReadFile(fpath)
		if rerr != nil {
			continue
		}
		fixtures[rel] = true
		if !fixtureScanExt[strings.ToLower(path.Ext(rel))] {
			continue
		}
		dir := path.Dir(rel)
		for _, loc := range externalRefs(data) {
			if loc == "" || isAbsoluteURL(loc) {
				continue
			}
			next := path.Clean(joinBase(dir+"/", loc))
			if !enqueued[next] {
				enqueued[next] = true
				queue = append(queue, next)
			}
		}
	}
}

func externalRefs(data []byte) []string {
	var refs []string
	for _, m := range systemRefRe.FindAllSubmatch(data, -1) {
		refs = append(refs, firstNonEmpty(m[1], m[2]))
	}
	for _, m := range publicRefRe.FindAllSubmatch(data, -1) {
		refs = append(refs, firstNonEmpty(m[1], m[2]))
	}
	return refs
}

func firstNonEmpty(a, b []byte) string {
	if len(a) > 0 {
		return string(a)
	}
	return string(b)
}

// parseTopCatalog reads xmlconf.xml, mapping each TESTCASES block's xml:base to
// the collection catalog its external entity reference expands to.
//
// The top catalog is a flat list of sibling <TESTCASES xml:base="…"> blocks,
// each holding one or more external general-entity references that expand to a
// per-collection catalog file. Go's encoding/xml does not expand external
// entities, so this reads the entity→systemID map from the DOCTYPE and pairs
// each TESTCASES block's xml:base with the catalogs its entity references name.
func parseTopCatalog(xmlconfRoot string) ([]collectionRef, error) {
	topPath, err := generator.ContainedPath(xmlconfRoot, "xmlconf.xml")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(topPath)
	if err != nil {
		return nil, fmt.Errorf("read xmlconf.xml: %w", err)
	}

	entities := map[string]string{}
	for _, m := range entityDeclRe.FindAllSubmatch(data, -1) {
		entities[string(m[1])] = string(m[2])
	}

	var refs []collectionRef
	for _, block := range testcasesBlockRe.FindAllSubmatch(data, -1) {
		base := ""
		if bm := xmlBaseRe.FindSubmatch(block[1]); bm != nil {
			base = string(bm[1])
		}
		for _, em := range entityRefRe.FindAllSubmatch(block[2], -1) {
			if sys := entities[string(em[1])]; sys != "" {
				refs = append(refs, collectionRef{Base: base, CatalogFile: path.Clean(sys)})
			}
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no collection references found in xmlconf.xml")
	}
	return refs, nil
}

// resolveFixture resolves a TEST @URI/@OUTPUT to a suite-root-relative path.
// It resolves against the inherited xml:base first, but falls back to the
// catalog file's own directory when the xml:base result is absent on disk and
// the catalog-relative one exists — the eduni "misc" block in xmlconf.xml carries
// an xml:base (eduni/namespaces/misc/) that does not match where its fixtures
// actually live (eduni/misc/, next to the catalog), and the files are only
// reachable relative to the catalog.
func resolveFixture(xmlconfRoot, base, catalogDir, ref string) string {
	primary := path.Clean(joinBase(base, ref))
	if fileExists(xmlconfRoot, primary) {
		return primary
	}
	alt := path.Clean(joinBase(catalogDir+"/", ref))
	if alt != primary && fileExists(xmlconfRoot, alt) {
		return alt
	}
	return primary
}

func fileExists(xmlconfRoot, rel string) bool {
	fpath, err := generator.ContainedPath(xmlconfRoot, rel)
	if err != nil {
		return false
	}
	if _, err := os.Stat(fpath); err != nil {
		return false
	}
	return true
}

// joinBase joins an xml:base directory prefix with a relative reference. base is
// treated as a directory (a trailing slash is implied); an empty base yields ref
// unchanged.
func joinBase(base, ref string) string {
	if base == "" {
		return ref
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return path.Join(base, ref)
}

func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "ftp://") || strings.HasPrefix(s, "urn:")
}

// FixtureRelsUnion returns every fixture rel referenced by the given cases,
// sorted and de-duplicated. Used by Fetch to copy fixtures.
func FixtureRelsUnion(byCollection map[string][]genCase) []string {
	rels := map[string]bool{}
	for _, cases := range byCollection {
		for _, c := range cases {
			for _, rel := range c.FixtureRels {
				rels[rel] = true
			}
		}
	}
	out := make([]string, 0, len(rels))
	for rel := range rels {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}
