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
```

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
