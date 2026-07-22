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
	"xsd10": {
		pkg:            "./xsd",
		rootTest:       "TestXSD10W3C",
		runPattern:     "^TestXSD10W3C$",
		junitSuite:     "xsd10-conformance",
		displayName:    "XSD 1.0",
		defaultOut:     "test-results/xsd10-junit.xml",
		defaultSummary: "test-results/xsd10-summary.md",
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
	"xml": {
		pkg:            "./xml",
		rootTest:       "TestXMLW3C",
		runPattern:     "^TestXMLW3C$",
		junitSuite:     "xml-conformance",
		displayName:    "XML 1.0/1.1",
		defaultOut:     "test-results/xml-junit.xml",
		defaultSummary: "test-results/xml-summary.md",
	},
	"xmldsig2ed": {
		pkg:            "./xmldsig",
		rootTest:       "TestXMLDSig2EdW3C",
		runPattern:     "^TestXMLDSig2EdW3C$",
		junitSuite:     "xmldsig2ed-conformance",
		displayName:    "XMLDSig2Ed (C14N 1.1 interop)",
		defaultOut:     "test-results/xmldsig2ed-junit.xml",
		defaultSummary: "test-results/xmldsig2ed-summary.md",
	},
	"xmldsig11": {
		pkg:            "./xmldsig",
		rootTest:       "TestXMLDSig11W3C",
		runPattern:     "^TestXMLDSig11W3C$",
		junitSuite:     "xmldsig11-conformance",
		displayName:    "XMLDSig 1.1 interop",
		defaultOut:     "test-results/xmldsig11-junit.xml",
		defaultSummary: "test-results/xmldsig11-summary.md",
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
	heliumCommit := fs.String("helium-commit", "", "helium (code under test) commit the suite ran against, recorded in the summary provenance")
	harnessCommit := fs.String("harness-commit", "", "helium-w3c-tests (harness) commit that produced the run, recorded in the summary provenance")
	goVersion := fs.String("go-version", "", "Go version/toolchain that ran the suite (e.g. the output of `go version`), recorded in the summary provenance")
	mode := fs.String("mode", "", "run mode recorded in the summary provenance (e.g. \"default\" or \"slow (HELIUM_SLOW_TESTS=1)\")")
	noSystemOut := fs.Bool("no-system-out", false, "omit per-testcase <system-out> from the JUnit XML (failure/skip diagnostics are kept); shrinks large reports")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: w3ctest [-out FILE] [-summary FILE] [-root DIR] <suite> [go test flags...]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "suites: qt3 xsd10 xsd11 xslt30 xml xmldsig2ed xmldsig11")
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
		SuiteName:     suite.junitSuite,
		RootTest:      suite.rootTest,
		OmitSystemOut: *noSystemOut,
	})
	closeErr := file.Close()
	if convertErr != nil {
		return 1, convertErr
	}
	if closeErr != nil {
		return 1, closeErr
	}
	fmt.Fprintf(os.Stderr, "wrote JUnit results to %s\n", *out)

	prov := provenance{
		heliumCommit:  *heliumCommit,
		harnessCommit: *harnessCommit,
		goVersion:     *goVersion,
		mode:          *mode,
	}
	if sErr := writeSummary(*summaryOut, *root, prov, suiteKey, suite, stdout.Bytes()); sErr != nil {
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

// provenance carries the run-context stamps recorded in the summary header so
// a committed evidence file stands alone: which helium code, which harness,
// which Go toolchain, and which run mode produced the numbers.
type provenance struct {
	heliumCommit  string
	harnessCommit string
	goVersion     string
	mode          string
}

// writeSummary rolls up the go-test-json output into a committed
// conformance-evidence markdown report, stamping provenance (pinned upstream
// commit from suites.lock, helium/harness commits, Go version, run mode,
// generation date) so the file stands alone.
func writeSummary(path, root string, prov provenance, suiteKey string, suite suiteConfig, jsonOut []byte) error {
	summary, err := junit.Summarize(bytes.NewReader(jsonOut), junit.Options{
		SuiteName: suiteKey,
		RootTest:  suite.rootTest,
	})
	if err != nil {
		return err
	}
	meta := junit.SummaryMeta{
		DisplayName:   suite.displayName,
		HeliumCommit:  prov.heliumCommit,
		HarnessCommit: prov.harnessCommit,
		GoVersion:     prov.goVersion,
		Mode:          prov.mode,
		GeneratedAt:   time.Now().UTC().Format("2006-01-02"),
	}
	if lock, lerr := generator.LoadLock(root); lerr == nil {
		if sl, ok := lock.Suite(suiteKey); ok {
			switch {
			case sl.Repo != "":
				meta.UpstreamRepo = strings.TrimSuffix(sl.Repo, ".git")
				meta.UpstreamCommit = sl.Commit
			case sl.URL != "":
				// Zip-pinned suite (the W3C xmlts archive): record the download
				// URL and the pinned sha256 as provenance in place of repo+commit.
				meta.UpstreamRepo = sl.URL
				if sl.Sha256 != "" {
					meta.UpstreamCommit = "sha256:" + sl.Sha256
				}
			}
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
