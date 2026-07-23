# Helium W3C Tests

Agent-consumed guidance. Keep terse. Update when repo workflow or suite policy changes.

## Purpose

- This module holds heavyweight W3C-derived conformance tests for `github.com/lestrrat-go/helium`.
- Main Helium module owns focused unit/regression tests.
- This module owns generated W3C tests, pinned upstream source checkouts, generated/pruned fixtures, and expectation metadata.
- `go.mod` uses `replace github.com/lestrrat-go/helium => ../helium`.

## Read First

- Read `README.md` before changing commands, layout, or suite ownership.
- Read `../helium/AGENTS.md` before changing suite semantics copied from Helium.
- Read suite generator code before editing generated tests.

## Layout

| Path | Purpose |
|------|---------|
| `cmd/w3cgen/` | fetch/generate/verify CLI |
| `cmd/w3ctest/` | conformance test runner; writes JUnit XML |
| `internal/generator/` | shared lock, fetch, catalog, write support |
| `internal/suites/qt3/` | QT3/XPath 3.1 generator policy |
| `internal/suites/xslt30/` | XSLT 3.0 generator policy |
| `internal/suites/xsd11/` | XSD 1.1 generator policy |
| `internal/suites/xml/` | XML conformance (parser) generator policy |
| `internal/suites/xmldsig2ed/` | C14N 1.1 + XMLDSig interop generator policy |
| `internal/suites/xmldsig11/` | XML Signature 1.1 interop generator policy |
| `internal/suites/merlinxmldsig/` | Merlin 2002 XMLDSig baseline generator policy |
| `expectations/` | skip/expected-failure metadata |
| `sources/` | upstream W3C checkouts; gitignored |
| `testdata/` | generated/pruned fixtures |
| `xpath3/` | generated QT3 tests |
| `xslt3/` | generated XSLT 3.0 tests |
| `xsd/` | generated XSD 1.1 tests |
| `xml/` | generated XML conformance tests |
| `xmldsig/` | generated XMLDSig2Ed + XMLDSig 1.1 interop tests |
| `fixtures/xmldsig2ed/` | vendored C14N 1.1 + XMLDSig interop vectors (committed) |

## Commands

- Fetch pinned suites: `go run ./cmd/w3cgen fetch qt3 xslt30 xsd11 xml xmldsig2ed xmldsig11 merlinxmldsig`
- Generate tests: `go run ./cmd/w3cgen generate all`
- Verify generated files: `go run ./cmd/w3cgen verify all`
- Run tests: `go test ./...`
- Run a suite's conformance with JUnit XML: `go run ./cmd/w3ctest xsd11` / `go run ./cmd/w3ctest xslt30` / `go run ./cmd/w3ctest xml` / `go run ./cmd/w3ctest xmldsig2ed` / `go run ./cmd/w3ctest xmldsig11` / `go run ./cmd/w3ctest merlinxmldsig`
- Default outputs: JUnit `test-results/<suite>-junit.xml` (override `-out FILE`) and summary markdown `test-results/<suite>-summary.md` (override `-summary FILE`). Point them at the helium repo to refresh its committed conformance evidence.
- Summary rolls up pass/skip/fail + skip-reason breakdown, stamped with the pinned upstream commit + date (point-in-time snapshot; regenerate to refresh).
- JUnit report contains conformance subtests only (`TestXSD11W3C/<case ID>`, `TestXSLT30W3C/<case name>`); manifest checks are excluded; skipped cases carry reason in `<skipped message>`.
- Source trees may be absent → generated manifest tests should skip, not fail.

## Go Workspace

- Use `go.work` for local multi-module testing with sibling Helium checkout.
- Create from this module root: `go work init . ../helium`
- Existing workspace → add modules: `go work use . ../helium`
- Verify workspace mode: `go env GOWORK`
- Keep `go.work` local unless user explicitly asks to version workspace state.
- `go.mod` already has `replace github.com/lestrrat-go/helium => ../helium`; keep replacement unless changing module linkage intentionally.
- To run a suite against an isolated Helium worktree (parallel conformance work), see README "Running Against a Helium Worktree": paired same-named worktrees in both repos, a gitignored `go.work` whose own `replace` overrides the `go.mod` `replace => ../helium` (a `use`-only `go.work` is NOT enough), a symlink of `testdata/xslt30` into the worktree (`RepoRoot()` resolves to the worktree via `runtime.Caller`), then `go test` from the worktree.

## Generated Files

- NEVER edit `*_gen_test.go` manually.
- Change generator code or expectation metadata, then run `go run ./cmd/w3cgen generate all`.
- Run `go run ./cmd/w3cgen verify all` before handoff.

## Suite Policy

- QT3 targets XPath 3.1 with XSD 1.1 behavior.
- Skip QT3 tests requiring XSD 1.0-only behavior.
- XSLT 3.0 targets Basic XSLT Processor conformance, including backwards-compatible processing (XSLT 1.0 behavior + XPath 1.0 compatibility mode). `featureSupported("backwards_compatibility")` is true; a few residual cases are skipped per-case in `expectations/xslt30.json` with specific reasons (1.0-only output method, base-uri fixture, XPath 1.0 grammar).
- The `spec="XSLT20"`-only tests stay out of scope (`specSupported` whitelists `XSLT20+`/`XSLT30`, not bare `XSLT20`); those are 2.0-specific expected outputs, not a runnable bucket for a 3.0 processor.
- XSD suite is pinned to w3c/xsdtests (git). `fetch xsd11` clones `sources/xsd11` and copies the XSD-1.1 fixtures into `testdata/xsd11` (gitignored); generated tests skip when fixtures are absent.
- XSLT suite is pinned to w3c/xslt30-test (git). `fetch xslt30` clones `sources/xslt30` and copies catalog-referenced fixtures (stylesheets + xsl:include/import closure, sources, packages, schemas, collections) into `testdata/xslt30` (gitignored), then overlays the committed `fixtures/xslt30` tree on top.
- XML suite is the W3C XML Conformance Test Suite, pinned to the `xmlts` zip (sha256 in `suites.lock.json`, `type: "zip"` — a download+verify+extract source kind in `internal/generator/fetch.go`). `fetch xml` extracts into `sources/xml/xmlconf` and copies each TEST document, its OUTPUT, and the transitive external DTD/entity closure into `testdata/xml` (gitignored). Targets **XML 1.0 + Namespaces 1.0**; the runner drives helium's `helium.Parser` as a validating processor (external DTD + entities loaded, `ValidateDTD` on for valid/invalid) and asserts each TEST's TYPE (valid → parses clean; invalid → validity error; not-wf → fatal error; error → optional, never fails). XML 1.1 / Namespaces 1.1 cases are gated off (`xml11Supported = false` in `xml/xml_harness_test.go`) until 1.1 parser support lands. Tests tagged for XML 1.0 editions that exclude the one helium implements — `EDITION="1 2 3 4"`, i.e. the 4th-edition enumerated name character classes (productions [85]–[89]) that the 5th edition replaced with broad NameStartChar/NameChar ranges — are gated too (`targetEdition = "5"`); every such case is accepted by libxml2, confirming the divergence is edition, not a helium defect. Current helium gaps (partial DTD validation, some DTD-internal WF checks) are recorded as categorized xfails in `expectations/xml.json`; the harness errors on an unexpected pass so fixes are caught.
- `xmldsig2ed` is the W3C Note "Test Cases for C14N 1.1 and XMLDSig Interoperability" (10 June 2008). No upstream archive, so the harness-consumed subset is **vendored** in `fixtures/xmldsig2ed` (provenance + sha256 in its README). Lock kind is `manual`: `fetch xmldsig2ed` overlays the fixtures into gitignored `testdata/xmldsig2ed` (no network; it does NOT call `FetchSource`, which errors on `manual`). Two case kinds share the `xmldsig/` leaf package (`TestXMLDSig2EdW3C`): 20 pure Canonical XML 1.1 node-set cases (XPath-1.0 node-set via `xpath1`, canonicalize `C14N11` via `c14n`, byte-compare) and 17 signature-verification cases, all passing. `sig-defCan-2/3` configure `xmldsig1/transform.XSLT` and exercise repeated node-set/octet transitions. `sig-defCan-1`'s Reference is an external document (`c14n11/xml-base-input.xml`, vendored); the sig runner supplies `xmldsig1.FSReferenceResolver(os.DirFS(testdataRoot))` and xmldsig1 joins the relative URI against each doc's BaseURI, so it verifies. The `c14n11/appendixa` RFC 3986 dot-segment pairs are vendored for provenance only (helium's URI join is covered elsewhere); they drive no cases.
  - The 8 `dname` cases carry only an X509 Distinguished Name (`ds:X509SubjectName`) in KeyInfo. Helium surfaces that string VERBATIM (no DName canonicalization); the harness (`keyResolver.matchDNameCert` in `xmldsig/harness_test.go`) decodes the RFC 2253/4514 escapes to attribute VALUES and selects the vendored cert whose `crypto/x509` Subject matches. Matching on decoded values (not the escaped string) is what makes the escaping-divergence cases (e.g. `dnString-4`, `Trailing\20\20` vs `Trailing \ `) pass. The certs live in `fixtures/xmldsig2ed/xmldsig/dname/certs` (`*.crt` + `keystore.p12`, password `secret`; the p12 is vendored for provenance only — verify needs only the public certs).
- `xmldsig11` is the W3C "XML Signature 1.1 Interop" enveloping-signature vectors. The canonical w3.org directory is member-gated, so the suite is pinned (git) to an Apache Santuario release tag (Apache-2.0 mirror) and scoped to the **oracle** vendor set — the only vendor the santuario driver exercises. `fetch xmldsig11` clones `sources/xmldsig11` and copies `oracle/signature-enveloping-*.xml` into gitignored `testdata/xmldsig11`. All 33 cases are verify-only (`TestXMLDSig11W3C`, HMAC key `testkey`, public keys built from inline KeyInfo) and pass (RSA/ECDSA/HMAC SHA-1..512, P-256/384/521, and RFC 4050 ECDSAKeyValue KeyInfo). No `HMACOutputLength`-truncation vectors exist in the oracle tree.
- `merlinxmldsig` is the 2002 IETF/W3C baseline "merlin-xmldsig-twenty-three" collection (Baltimore/Merlin Hughes) the two newer suites build on. It ships inside the **same** santuario checkout `xmldsig11` pins, so its lock entry reuses that repo/commit and `sourceDir` (`sources/xmldsig11`), like xsd10/xsd11 — one clone feeds both. `fetch merlinxmldsig` ensures the shared clone, then copies the 16 signed documents + `certs/` into gitignored `testdata/merlinxmldsig`, plus the two external-reference targets the vectors sign against: `xml-stylesheet.b64` (ships in the collection) and `xml-stylesheet` (Santuario's copy of `http://www.w3.org/TR/xml-stylesheet`, which lives OUTSIDE the collection in the apache test resources, so the generator copies it separately). All 16 verify-only cases pass (`TestMerlinXMLDSigW3C`, HMAC key `secret`). The external-reference cases verify via `merlinReferenceResolver`, a harness resolver mapping the two absolute W3C URLs to the vendored local files (helium's `FSReferenceResolver` refuses scheme URIs, so an explicit URL-to-file map is needed). `signature-enveloping-hmac-sha1-40` is deliberately INVALID (40-bit-truncated HMACOutputLength) — helium's fail-closed rejection satisfies the must-reject, modeled by the case table's `MustFail` flag. The harness resolves `KeyName`, `X509SKI`, `RetrievalMethod`, and the `signature-x509-sn`/`-is`/`-crt` forms through the inline or out-of-band certificate pool.
- The three xmldsig suites emit **no suite-level manifest test**: they share one leaf package and `generator.ManifestTestSource`'s fixed-named consts would collide. Per-case skips point at the fetch command when testdata is absent.
- `fixtures/xslt30` (committed) holds the small curated fixture set the catalog scan cannot reproduce from the raw clone: files referenced only at run time (doc()/unparsed-text(), dynamic fn:transform stylesheets, collection members) and fixtures whose content was hand-edited (e.g. a DTD with its XML declaration stripped). Regenerate it only from a known-good fixture tree; never delete a file here without confirming no case needs it.

## Expectations

- Use `expectations/*.json` for persistent skips/xfails.
- Prefer explicit reason strings for unsupported specs/features.
- Do not hide generator/parser bugs as expectations without root-cause notes.

## Safety

- Treat catalog paths as untrusted upstream data.
- Resolve catalog file references through containment checks before reading/copying.
- Keep `sources/`, `testdata/` bulk fixture churn out of unrelated changes.
