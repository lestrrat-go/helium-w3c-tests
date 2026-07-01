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
	"strings"
	"time"

	"github.com/lestrrat-go/helium-w3c-tests/internal/generator"
	"github.com/lestrrat-go/helium-w3c-tests/internal/junit"
)

// suiteConfig describes how to run and report one conformance suite.
type suiteConfig struct {
	pkg            string // go test package path
	rootTest       string // root test whose subtests are the cases
	runPattern     string // -run regexp
	junitSuite     string // <testsuite name> in the JUnit output
	displayName    string // human name for the summary title
	defaultOut     string // default -out (JUnit) path
	defaultSummary string // default -summary (markdown) path
}

var suites = map[string]suiteConfig{
	"qt3": {
		pkg:            "./xpath3",
		rootTest:       "TestQT3W3C",
		runPattern:     "^TestQT3W3C$",
		junitSuite:     "qt3-conformance",
		displayName:    "QT3 (XPath 3.1)",
		defaultOut:     "test-results/qt3-junit.xml",
		defaultSummary: "test-results/qt3-summary.md",
	},
	"xsd11": {
		pkg:            "./xsd",
		rootTest:       "TestXSD11W3C",
		runPattern:     "^TestXSD11W3C$",
		junitSuite:     "xsd11-conformance",
		displayName:    "XSD 1.1",
		defaultOut:     "test-results/xsd11-junit.xml",
		defaultSummary: "test-results/xsd11-summary.md",
	},
	"xslt30": {
		pkg:            "./xslt3",
		rootTest:       "TestXSLT30W3C",
		runPattern:     "^TestXSLT30W3C$",
		junitSuite:     "xslt30-conformance",
		displayName:    "XSLT 3.0",
		defaultOut:     "test-results/xslt30-junit.xml",
		defaultSummary: "test-results/xslt30-summary.md",
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
	summaryOut := fs.String("summary", "", "conformance summary markdown path (default: per-suite under test-results/)")
	root := fs.String("root", ".", "module root containing suites.lock.json")
	heliumCommit := fs.String("helium-commit", "", "helium commit the suite ran against, recorded in the summary provenance")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: w3ctest [-out FILE] [-summary FILE] [-root DIR] <suite> [go test flags...]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "suites: qt3 xsd11 xslt30")
	}
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	argv := fs.Args()
	if len(argv) == 0 {
		fs.Usage()
		return 2, errors.New("missing suite")
	}
	suiteKey := argv[0]
	suite, ok := suites[suiteKey]
	if !ok {
		fs.Usage()
		return 2, fmt.Errorf("unknown suite %q", suiteKey)
	}
	if *out == "" {
		*out = suite.defaultOut
	}
	if *summaryOut == "" {
		*summaryOut = suite.defaultSummary
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

	if sErr := writeSummary(*summaryOut, *root, *heliumCommit, suiteKey, suite, stdout.Bytes()); sErr != nil {
		return 1, sErr
	}

	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return 1, err
}

// writeSummary rolls up the go-test-json output into a committed
// conformance-evidence markdown report, stamping provenance (pinned upstream
// commit from suites.lock, generation date) so the file stands alone.
func writeSummary(path, root, heliumCommit, suiteKey string, suite suiteConfig, jsonOut []byte) error {
	summary, err := junit.Summarize(bytes.NewReader(jsonOut), junit.Options{
		SuiteName: suiteKey,
		RootTest:  suite.rootTest,
	})
	if err != nil {
		return err
	}
	meta := junit.SummaryMeta{
		DisplayName:  suite.displayName,
		HeliumCommit: heliumCommit,
		GeneratedAt:  time.Now().UTC().Format("2006-01-02"),
	}
	if lock, lerr := generator.LoadLock(root); lerr == nil {
		if sl, ok := lock.Suite(suiteKey); ok {
			meta.UpstreamRepo = strings.TrimSuffix(sl.Repo, ".git")
			meta.UpstreamCommit = sl.Commit
		}
	}

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return mkErr
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if werr := junit.WriteSummaryMarkdown(f, summary, meta); werr != nil {
		_ = f.Close()
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	fmt.Fprintf(os.Stderr, "wrote conformance summary to %s\n", path)
	return nil
}
