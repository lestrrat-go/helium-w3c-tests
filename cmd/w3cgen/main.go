package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/merlinxmldsig"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/qt3"
	xmlsuite "github.com/lestrrat-go/helium-w3c-tests/internal/suites/xml"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/xmldsig11"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/xmldsig2ed"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/xsd11"
	"github.com/lestrrat-go/helium-w3c-tests/internal/suites/xslt30"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "w3cgen: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("w3cgen", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repository root containing suites.lock.json")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: w3cgen [-root DIR] <list|fetch|generate|verify> [all|suite...]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "suites: qt3 xslt30 xsd11 xml xmldsig2ed xmldsig11 merlinxmldsig")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	argv := fs.Args()
	if len(argv) == 0 {
		fs.Usage()
		return errors.New("missing command")
	}

	root, err := generator.AbsRoot(*rootFlag)
	if err != nil {
		return err
	}
	lock, err := generator.LoadLock(root)
	if err != nil {
		return err
	}

	registry := generator.NewRegistry(qt3.New(), xslt30.New(), xsd11.New(), xmlsuite.New(), xmldsig2ed.New(), xmldsig11.New(), merlinxmldsig.New())
	genCtx := generator.Context{
		Root: root,
		Lock: lock,
	}

	switch argv[0] {
	case "list":
		for _, name := range registry.Names() {
			suiteLock, ok := lock.Suite(name)
			if !ok {
				fmt.Printf("%s missing-lock\n", name)
				continue
			}
			fmt.Printf("%s %s %s\n", name, suiteLock.SourceKind(), suiteLock.SourceDir)
		}
		return nil
	case "fetch":
		selected, err := registry.Select(argv[1:])
		if err != nil {
			return err
		}
		for _, suite := range selected {
			suiteLock, ok := lock.Suite(suite.Name())
			if !ok {
				return fmt.Errorf("suite %q is not present in suites.lock.json", suite.Name())
			}
			if err := suite.Fetch(ctx, genCtx, suiteLock); err != nil {
				return err
			}
		}
		return nil
	case "generate", "verify":
		mode := generator.ModeWrite
		if argv[0] == "verify" {
			mode = generator.ModeVerify
		}
		selected, err := registry.Select(argv[1:])
		if err != nil {
			return err
		}
		for _, suite := range selected {
			suiteLock, ok := lock.Suite(suite.Name())
			if !ok {
				return fmt.Errorf("suite %q is not present in suites.lock.json", suite.Name())
			}
			if err := suite.Generate(ctx, genCtx, suiteLock, mode); err != nil {
				return err
			}
		}
		return nil
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", argv[0])
	}
}
