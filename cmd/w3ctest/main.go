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

const (
	defaultOut      = "test-results/xsd11-junit.xml"
	xsd11SuiteName  = "xsd11-conformance"
	xsd11RootTest   = "TestXSD11W3C"
	xsd11RunPattern = "^TestXSD11W3C$"
)

func main() {
	code, err := run(context.Background(), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "w3ctest: %v\n", err)
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string) (int, error) {
	fs := flag.NewFlagSet("w3ctest", flag.ContinueOnError)
	out := fs.String("out", defaultOut, "JUnit XML output path")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: w3ctest [-out FILE] xsd11 [go test flags...]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintf(fs.Output(), "default output: %s\n", defaultOut)
	}
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	argv := fs.Args()
	if len(argv) == 0 {
		fs.Usage()
		return 2, errors.New("missing suite")
	}
	if argv[0] != "xsd11" {
		fs.Usage()
		return 2, fmt.Errorf("unknown suite %q", argv[0])
	}

	testArgs := []string{"test", "-json", "-run", xsd11RunPattern}
	testArgs = append(testArgs, argv[1:]...)
	testArgs = append(testArgs, "./xsd")

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
		SuiteName: xsd11SuiteName,
		RootTest:  xsd11RootTest,
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
