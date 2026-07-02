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
| `expectations/` | skip/expected-failure metadata |
| `sources/` | upstream W3C checkouts; gitignored |
| `testdata/` | generated/pruned fixtures |
| `xpath3/` | generated QT3 tests |
| `xslt3/` | generated XSLT 3.0 tests |
| `xsd/` | generated XSD 1.1 tests |

## Commands

- Fetch pinned suites: `go run ./cmd/w3cgen fetch qt3 xslt30 xsd11`
- Generate tests: `go run ./cmd/w3cgen generate all`
- Verify generated files: `go run ./cmd/w3cgen verify all`
- Run tests: `go test ./...`
- Run a suite's conformance with JUnit XML: `go run ./cmd/w3ctest xsd11` / `go run ./cmd/w3ctest xslt30`
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
- `fixtures/xslt30` (committed) holds the small curated fixture set the catalog scan cannot reproduce from the raw clone: files referenced only at run time (doc()/unparsed-text(), dynamic fn:transform stylesheets, collection members) and fixtures whose content was hand-edited (e.g. a DTD with its XML declaration stripped). Regenerate it only from a known-good fixture tree; never delete a file here without confirming no case needs it.

## Expectations

- Use `expectations/*.json` for persistent skips/xfails.
- Prefer explicit reason strings for unsupported specs/features.
- Do not hide generator/parser bugs as expectations without root-cause notes.

## Safety

- Treat catalog paths as untrusted upstream data.
- Resolve catalog file references through containment checks before reading/copying.
- Keep `sources/`, `testdata/` bulk fixture churn out of unrelated changes.
