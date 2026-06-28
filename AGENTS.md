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
- XSLT 3.0 targets Basic XSLT Processor conformance.
- Do not implement or unskip XSLT 1.0/2.0 backwards compatibility tests.
- XSD suite is pinned to w3c/xsdtests (git). `fetch xsd11` clones `sources/xsd11` and copies the XSD-1.1 fixtures into `testdata/xsd11` (gitignored); generated tests skip when fixtures are absent.

## Expectations

- Use `expectations/*.json` for persistent skips/xfails.
- Prefer explicit reason strings for unsupported specs/features.
- Do not hide generator/parser bugs as expectations without root-cause notes.

## Safety

- Treat catalog paths as untrusted upstream data.
- Resolve catalog file references through containment checks before reading/copying.
- Keep `sources/`, `testdata/` bulk fixture churn out of unrelated changes.
