# Helium W3C Tests

This repository holds heavyweight W3C-derived conformance tests for
`github.com/lestrrat-go/helium`.

The main Helium module should keep only focused unit and regression tests. This
repository owns generated W3C test files, pinned upstream suite checkouts, copied
fixtures, and expectation metadata.

## Layout

```
cmd/w3cgen/          unified fetch/generate/verify command
internal/generator/  common suite registry, lock-file, fetch, write support
internal/suites/     suite-specific generators
expectations/        skip and expected-failure metadata
sources/             upstream W3C clones, gitignored
testdata/            generated/pruned fixtures
xpath3/              QT3 generated tests
xslt3/               XSLT 3.0 generated tests
xsd/                 XSD 1.1 generated tests
xml/                 XML conformance (parser) generated tests
xmldsig/             XMLDSig2Ed + XMLDSig 1.1 + Merlin baseline interop tests
fixtures/xmldsig2ed/ vendored C14N 1.1 + XMLDSig interop vectors
```

## Local Development

This module is intended to sit next to Helium:

```
github.com/lestrrat-go/
  helium/
  helium-w3c-tests/
```

`go.mod` uses a local `replace` to test the sibling Helium checkout.

## Commands

Fetch pinned upstream suites:

```sh
go run ./cmd/w3cgen fetch qt3
go run ./cmd/w3cgen fetch xslt30
go run ./cmd/w3cgen fetch xsd11
go run ./cmd/w3cgen fetch xml
go run ./cmd/w3cgen fetch xmldsig2ed
go run ./cmd/w3cgen fetch xmldsig11
go run ./cmd/w3cgen fetch merlinxmldsig
```

The `xmldsig2ed` suite is the W3C Note "Test Cases for C14N 1.1 and XMLDSig
Interoperability" (10 June 2008). It has no upstream archive, so the
harness-consumed vectors are vendored (committed) under `fixtures/xmldsig2ed`
(provenance and sha256 in that directory's README); its `manual` fetch overlays
them into the gitignored `testdata/xmldsig2ed`. It exercises helium's `c14n`,
`xpath1`, and `xmldsig1` packages: 20 pure Canonical XML 1.1 node-set cases plus
17 signature-verification cases, all passing. The `defCan` multi-phase cases
configure the opt-in `xmldsig1/transform.XSLT` adapter. External-document
References resolve through `xmldsig1.FSReferenceResolver` (its target is
vendored); the dname cases carry only an X509 Distinguished Name in KeyInfo, so
the harness selects the signing cert from vendored certs by decoded-DName match.
The `xmldsig11` suite is the
W3C "XML Signature 1.1 Interop" enveloping-signature vectors, pinned to an Apache
Santuario release (the public Apache-2.0 mirror of the member-gated w3.org
directory) and scoped to the oracle vendor set: 33 verify-only cases, all
passing. The `merlinxmldsig` suite is the 2002 Baltimore/Merlin
"merlin-xmldsig-twenty-three" baseline collection, which ships inside the same
Santuario checkout (so its lock entry reuses that clone): 16 verify-only signed
documents, all passing. This includes DSA/RSA/HMAC-SHA1,
base64/external-reference cases resolved through a harness URL-to-file resolver,
KeyName/X509SKI/RetrievalMethod KeyInfo forms, the composite `signature.xml`, and
a deliberately-invalid truncated-HMAC vector that helium correctly rejects. Any
known gaps are recorded as categorized xfails in
`expectations/xmldsig2ed.json`, `expectations/xmldsig11.json`, and
`expectations/merlinxmldsig.json`.

The `xml` suite is the W3C XML Conformance Test Suite (parser well-formedness
and DTD validity). Unlike the git-pinned suites it is pinned to the W3C `xmlts`
zip by sha256 (a `zip` source kind that downloads, verifies, and extracts). It
targets XML 1.0 + Namespaces 1.0; XML 1.1 / Namespaces 1.1 cases are gated off
until 1.1 parser support lands. Known helium gaps are recorded as categorized
xfails in `expectations/xml.json`.

Regenerate tests:

```sh
go run ./cmd/w3cgen generate all
```

Verify generated files are current:

```sh
go run ./cmd/w3cgen verify all
```

Run tests:

```sh
go test ./...
```

Run a suite's conformance tests and write JUnit XML results:

```sh
go run ./cmd/w3ctest xsd11
go run ./cmd/w3ctest xslt30
go run ./cmd/w3ctest xml
go run ./cmd/w3ctest xmldsig2ed
go run ./cmd/w3ctest xmldsig11
go run ./cmd/w3ctest merlinxmldsig
```

Each run writes two files, both defaulting under `test-results/`: the JUnit XML
(`<suite>-junit.xml`, override with `-out`) and a human-readable conformance
**summary** (`<suite>-summary.md`, override with `-summary`). The summary rolls
up pass/skip/fail counts and a skip-reason breakdown, stamped with the pinned
upstream suite commit and the generation date:

```sh
go run ./cmd/w3ctest -out /tmp/xslt30-junit.xml -summary /tmp/xslt30.md xslt30
```

Point `-out`/`-summary` at the helium repo to refresh its committed conformance
evidence (the summary is a point-in-time snapshot; regenerate to update it).

The JUnit report contains conformance results only: one testcase per
`TestXSD11W3C/<case ID>` (or `TestXSLT30W3C/<case name>`) subtest. The manifest
availability check is excluded. Skipped conformance cases include the skip
reason in the JUnit `<skipped>` message.

Per-case skips for tests that are valid per spec but exercise behavior the
processor does not follow live in `expectations/<suite>.json`; structural
spec/feature/streaming gating is computed at generation time.

The XSLT 3.0 suite copies catalog-referenced fixtures out of the gitignored
`sources/xslt30` clone into `testdata/xslt30`, then overlays the committed
`fixtures/xslt30` tree — a small curated set (files referenced only at run time,
plus hand-edited fixtures) that a static catalog scan cannot reproduce.

## Running Against a Helium Worktree

The `go.mod` `replace` points at the sibling `../helium` root checkout. To run a
suite against a specific Helium worktree instead (e.g. a feature branch, so
conformance work can proceed in parallel), use paired worktrees plus a local
`go.work`:

1. Create worktrees with the SAME branch name in both repos:

   ```sh
   git -C <helium>           worktree add <helium>/.worktrees/<branch>           -b <branch> [origin/main]
   git -C <helium-w3c-tests> worktree add <helium-w3c-tests>/.worktrees/<branch> -b <branch> [origin/main]
   ```

2. In the helium-w3c-tests worktree, create a `go.work` (it is gitignored) whose
   `replace` overrides the `go.mod` `replace` and points at the Helium worktree:

   ```
   go 1.26.1

   use .

   replace github.com/lestrrat-go/helium => /abs/path/to/helium/.worktrees/<branch>
   ```

   A `use`-only `go.work` does NOT work: the `go.mod` `replace => ../helium`
   still fires and resolves to the root checkout. The `go.work` `replace` is
   required to override it.

3. Fixtures (`testdata/xslt30`, ~215MB, gitignored, fetched via
   `go run ./cmd/w3cgen fetch`) live only in the root checkout. Symlink them into
   the worktree instead of re-fetching — `RepoRoot()` resolves via
   `runtime.Caller` to the worktree, so the fixtures must be reachable locally:

   ```sh
   mkdir -p <helium-w3c-tests>/.worktrees/<branch>/testdata
   ln -s <helium-w3c-tests>/testdata/xslt30 <helium-w3c-tests>/.worktrees/<branch>/testdata/xslt30
   ```

4. Run tests from the worktree:

   ```sh
   cd <helium-w3c-tests>/.worktrees/<branch>
   go test ./xslt3/ -run 'TestXSLT30W3C/<case>' -v
   ```

## Current State

This is the infrastructure split. The suite plugins already produce executable
Go test files and read upstream catalog metadata when source checkouts are
present. The next migration step is to port the full in-tree QT3 and XSLT 3.0
catalog semantics into these plugins, then remove the generated W3C files and
large W3C fixtures from the main Helium module.
