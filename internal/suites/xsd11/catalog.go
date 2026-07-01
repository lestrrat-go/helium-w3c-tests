package xsd11

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
)

// The W3C XML Schema Test Suite (XSTS) catalog layout, restricted to the
// elements this generator consumes. The catalog is suite.xml at the source
// root; it references per-contributor *.testSet files via testSetRef/@href
// (an xlink:href; Go matches by local name). Each testSet contains testGroups,
// each with at most one schemaTest and any number of instanceTests.

type suiteDoc struct {
	TestSetRefs []testSetRef `xml:"testSetRef"`
}

type testSetRef struct {
	Href string `xml:"href,attr"`
}

type testSet struct {
	Name       string      `xml:"name,attr"`
	Version    string      `xml:"version,attr"`
	TestGroups []testGroup `xml:"testGroup"`
}

type testGroup struct {
	Name         string         `xml:"name,attr"`
	Version      string         `xml:"version,attr"`
	SchemaTest   *schemaTest    `xml:"schemaTest"`
	InstanceTest []instanceTest `xml:"instanceTest"`
}

type schemaTest struct {
	Documents []docRef   `xml:"schemaDocument"`
	Expected  []expected `xml:"expected"`
}

type instanceTest struct {
	Name     string     `xml:"name,attr"`
	Document docRef     `xml:"instanceDocument"`
	Expected []expected `xml:"expected"`
}

type docRef struct {
	Href string `xml:"href,attr"`
}

type expected struct {
	Validity string `xml:"validity,attr"`
	Version  string `xml:"version,attr"`
}

// genCase is the emitted per-group model: identity plus testdata-relative
// fixture paths. No fixture bytes are embedded.
type genCase struct {
	ID          string
	SchemaRel   string
	SchemaValid bool
	// SchemaDocs is the full list of schemaDocument rels declared by the
	// schemaTest, in catalog order (SchemaRel is SchemaDocs[0]). When a
	// schemaTest lists more than one document they together form a single
	// schema and the harness compiles all of them; a single-document test
	// leaves this at length 1 (or empty) and behaves exactly as before.
	SchemaDocs []string
	Instances  []genInstance
	// FixtureRels is the union of every fixture file this case references
	// (primary schema + transitive includes + instance documents), as clean
	// suite-root-relative slash paths. Used by Fetch to copy fixtures; not
	// emitted into the generated test files.
	FixtureRels []string
}

type genInstance struct {
	Name  string
	Rel   string
	Valid bool
}

var schemaLocationRe = regexp.MustCompile(`schemaLocation\s*=\s*["']([^"']+)["']`)

// readCases parses the suite catalog rooted at sourceRoot and returns the
// XSD-1.1 cases grouped by contributor (ibm, oracle, saxon, wg, ...). The
// returned contributor keys are sorted. setsScanned/groupsOut are counts for
// the manifest.
func readCases(sourceRoot string) (byContributor map[string][]genCase, contributors []string, setsScanned, groupsOut int, err error) {
	suiteBytes, err := os.ReadFile(filepath.Join(sourceRoot, "suite.xml"))
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("read suite.xml: %w", err)
	}
	var suite suiteDoc
	if err := xml.Unmarshal(suiteBytes, &suite); err != nil {
		return nil, nil, 0, 0, fmt.Errorf("parse suite.xml: %w", err)
	}

	byContributor = map[string][]genCase{}
	for _, ref := range suite.TestSetRefs {
		href := ref.Href
		if href == "" {
			continue
		}
		tsPath, perr := generator.ContainedPath(sourceRoot, href)
		if perr != nil {
			return nil, nil, 0, 0, fmt.Errorf("resolve testSet %q: %w", href, perr)
		}
		tsBytes, rerr := os.ReadFile(tsPath)
		if rerr != nil {
			// Missing testSet file: skip (the catalog may reference sets not
			// shipped in this checkout).
			continue
		}
		setsScanned++

		var ts testSet
		if uerr := xml.Unmarshal(tsBytes, &ts); uerr != nil {
			return nil, nil, 0, 0, fmt.Errorf("parse %s: %w", href, uerr)
		}

		tsDir := path.Dir(href)
		contributor := contributorName(href)

		for _, g := range ts.TestGroups {
			if !is11(g.Version, ts.Version) {
				continue
			}
			gc, ok, berr := buildCase(sourceRoot, href, tsDir, g)
			if berr != nil {
				return nil, nil, 0, 0, berr
			}
			if !ok {
				continue
			}
			byContributor[contributor] = append(byContributor[contributor], gc)
			groupsOut++
		}
	}

	for name := range byContributor {
		contributors = append(contributors, name)
	}
	sort.Strings(contributors)
	return byContributor, contributors, setsScanned, groupsOut, nil
}

// contributorName maps a testSetRef href's first path segment to a contributor
// key, stripping a trailing "Meta" (e.g. ibmMeta -> ibm, wgMeta -> wg).
func contributorName(href string) string {
	seg := href
	if i := strings.IndexByte(seg, '/'); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.TrimSuffix(seg, "Meta")
	if seg == "" {
		seg = "misc"
	}
	return seg
}

func is11(groupVersion, setVersion string) bool {
	return groupVersion == "1.1" || setVersion == "1.1"
}

// pickValidity selects the version-aware expected validity. Returns (valid, ok).
func pickValidity(exps []expected) (bool, bool) {
	if len(exps) == 0 {
		return false, false
	}
	var chosen *expected
	for i := range exps {
		if exps[i].Version == "1.1" {
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

// resolveRel resolves an href (relative to baseDir, both slash-style and
// suite-root-relative) into a clean suite-root-relative slash path.
func resolveRel(baseDir, href string) string {
	return path.Clean(path.Join(baseDir, href))
}

// buildCase assembles a genCase for a 1.1 testGroup, collecting the schema (and
// its transitive includes) plus all instance documents as suite-root-relative
// paths. Returns (case, ok); ok is false when there is no usable schemaTest or
// the primary schema is missing on disk.
func buildCase(sourceRoot, href, tsDir string, g testGroup) (genCase, bool, error) {
	if g.SchemaTest == nil || len(g.SchemaTest.Documents) == 0 {
		return genCase{}, false, nil
	}
	schemaValid, ok := pickValidity(g.SchemaTest.Expected)
	if !ok {
		return genCase{}, false, nil
	}

	primaryRel := resolveRel(tsDir, g.SchemaTest.Documents[0].Href)

	fixtures := map[string]bool{}

	// BFS over schema documents + transitive schemaLocation includes.
	queue := []string{}
	enqueued := map[string]bool{}
	add := func(rel string) {
		if !enqueued[rel] {
			enqueued[rel] = true
			queue = append(queue, rel)
		}
	}
	for _, d := range g.SchemaTest.Documents {
		add(resolveRel(tsDir, d.Href))
	}

	primaryFound := false
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		fpath, perr := generator.ContainedPath(sourceRoot, rel)
		if perr != nil {
			// Reference escapes the suite root: ignore it (untrusted catalog).
			continue
		}
		data, rerr := os.ReadFile(fpath)
		if rerr != nil {
			// Missing include: collect only what exists.
			continue
		}
		if rel == primaryRel {
			primaryFound = true
		}
		fixtures[rel] = true

		// s3_2_3ii05.xsd (and other IBM fixtures) are UTF-16 encoded; the
		// ASCII schemaLocationRe would find zero matches over the raw bytes
		// and silently drop the transitive import. Transcode any BOM-marked
		// UTF-16/UTF-8 content to plain UTF-8 first; ASCII/UTF-8 is unchanged.
		data = decodeToUTF8(data)

		dir := path.Dir(rel)
		for _, m := range schemaLocationRe.FindAllSubmatch(data, -1) {
			loc := string(m[1])
			if loc == "" || isAbsoluteURL(loc) {
				continue
			}
			add(resolveRel(dir, loc))
		}
	}

	if !primaryFound {
		return genCase{}, false, nil
	}

	schemaDocs := make([]string, 0, len(g.SchemaTest.Documents))
	for _, d := range g.SchemaTest.Documents {
		schemaDocs = append(schemaDocs, resolveRel(tsDir, d.Href))
	}

	gc := genCase{
		ID:          href + "/" + g.Name,
		SchemaRel:   primaryRel,
		SchemaValid: schemaValid,
		SchemaDocs:  schemaDocs,
	}

	for _, it := range g.InstanceTest {
		valid, ok := pickValidity(it.Expected)
		if !ok {
			continue
		}
		rel := resolveRel(tsDir, it.Document.Href)
		fpath, perr := generator.ContainedPath(sourceRoot, rel)
		if perr != nil {
			continue
		}
		if _, serr := os.Stat(fpath); serr != nil {
			// Missing instance: skip it.
			continue
		}
		fixtures[rel] = true
		gc.Instances = append(gc.Instances, genInstance{
			Name:  it.Name,
			Rel:   rel,
			Valid: valid,
		})
	}

	gc.FixtureRels = make([]string, 0, len(fixtures))
	for rel := range fixtures {
		gc.FixtureRels = append(gc.FixtureRels, rel)
	}
	sort.Strings(gc.FixtureRels)

	return gc, true, nil
}

func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// decodeToUTF8 returns data transcoded to UTF-8 when it carries a UTF-16 LE/BE
// or UTF-8 byte-order mark, and returns it unchanged otherwise. Only the BOM
// forms are handled — that is what the XSTS fixtures use — so plain ASCII/UTF-8
// content (the common case) passes through untouched.
func decodeToUTF8(data []byte) []byte {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16ToUTF8(data[2:], binary.LittleEndian)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16ToUTF8(data[2:], binary.BigEndian)
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return data[3:]
	default:
		return data
	}
}

// utf16ToUTF8 decodes UTF-16 code units (surrogate pairs included) in the given
// byte order into a UTF-8 byte slice.
func utf16ToUTF8(b []byte, order binary.ByteOrder) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, order.Uint16(b[i:i+2]))
	}
	return []byte(string(utf16.Decode(units)))
}
