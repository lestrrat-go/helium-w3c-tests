package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/lestrrat-go/helium-w3c-tests/internal/junit"
)

// suiteConfig describes how to run and report one conformance suite.
type suiteConfig struct {
	pkg        string // go test package path
	rootTest   string // root test whose subtests are the cases
	runPattern string // -run regexp
	junitSuite string // <testsuite name> in the JUnit output
	defaultOut string // default -out path
}

var suites = map[string]suiteConfig{
	"xsd11": {
		pkg:        "./xsd",
		rootTest:   "TestXSD11W3C",
		runPattern: "^TestXSD11W3C$",
		junitSuite: "xsd11-conformance",
		defaultOut: "test-results/xsd11-junit.xml",
	},
	"xslt30": {
		pkg:        "./xslt3",
		rootTest:   "TestXSLT30W3C",
		runPattern: "^TestXSLT30W3C$",
		junitSuite: "xslt30-conformance",
		defaultOut: "test-results/xslt30-junit.xml",
	},
}

func main() {
	code, err := run(context.Background(), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "w3ctest: %v\n", err)
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string) (int, error) {
	fs := flag.NewFlagSet("w3ctest", flag.ContinueOnError)
	out := fs.String("out", "", "JUnit XML output path (default: per-suite under test-results/)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: w3ctest [-out FILE] <suite> [go test flags...]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "suites: xsd11 xslt30")
	}
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	argv := fs.Args()
	if len(argv) == 0 {
		fs.Usage()
		return 2, errors.New("missing suite")
	}
	suite, ok := suites[argv[0]]
	if !ok {
		fs.Usage()
		return 2, fmt.Errorf("unknown suite %q", argv[0])
	}
	if *out == "" {
		*out = suite.defaultOut
	}

	testArgs := []string{"test", "-json", "-run", suite.runPattern}
	testArgs = append(testArgs, argv[1:]...)
	testArgs = append(testArgs, suite.pkg)

	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", testArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	if mkErr := os.MkdirAll(filepath.Dir(*out), 0o755); mkErr != nil {
		return 1, mkErr
	}
	file, createErr := os.Create(*out)
	if createErr != nil {
		return 1, createErr
	}
	convertErr := junit.ConvertGoTestJSON(bytes.NewReader(stdout.Bytes()), file, junit.Options{
		SuiteName: suite.junitSuite,
		RootTest:  suite.rootTest,
	})
	closeErr := file.Close()
	if convertErr != nil {
		return 1, convertErr
	}
	if closeErr != nil {
		return 1, closeErr
	}
	fmt.Fprintf(os.Stderr, "wrote JUnit results to %s\n", *out)

	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return 1, err
}
