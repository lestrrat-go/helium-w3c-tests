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

## Current State

This is the infrastructure split. The suite plugins already produce executable
Go test files and read upstream catalog metadata when source checkouts are
present. The next migration step is to port the full in-tree QT3 and XSLT 3.0
catalog semantics into these plugins, then remove the generated W3C files and
large W3C fixtures from the main Helium module.
