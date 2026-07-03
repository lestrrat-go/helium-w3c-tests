package xslt3_test

// This file is the machine-readable, CI-enforced conformance skip contract for
// the W3C XSLT 3.0 suite. It has three responsibilities:
//
//  1. w3cSkipDecision — the single, pure source of truth for whether a case is
//     skipped in a run and why. w3cRunOne (the live runner) and the ledger
//     generator both call it, so the ledger cannot drift from what actually
//     runs.
//  2. TestXSLT30SkipLedger with -update-ledger regenerates the checked-in
//     ledger (expectations/xslt30-skip-ledger.json) and count contract
//     (expectations/xslt30-skip-counts.json) from the real skip sources.
//  3. TestXSLT30SkipLedger without the flag is the fast, fixture-free CI
//     drift-check: it regenerates the ledger in memory and fails on any of the
//     five drift conditions documented on the test.
//
// Regenerate after an intentional skip change:
//
//	go test ./xslt3 -run TestXSLT30SkipLedger -update-ledger
//
//go:generate go test . -run TestXSLT30SkipLedger -update-ledger

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var updateLedger = flag.Bool("update-ledger", false,
	"regenerate the checked-in XSLT 3.0 skip ledger and count contract")

const (
	w3cSkipLedgerPath = "../expectations/xslt30-skip-ledger.json"
	w3cSkipCountsPath = "../expectations/xslt30-skip-counts.json"

	// w3cExpectedFail is the conformance contract: the suite has zero failures
	// in both the default and slow runs (see xslt3/CONFORMANCE.md). Actual
	// pass/fail is proven by the slow suite job; here it is asserted as a
	// committed constant so nobody can quietly commit a ledger that tolerates
	// failures.
	w3cExpectedFail = 0
)

// Skip-class labels. These are exactly the five buckets defined in
// helium xslt3/CONFORMANCE.md's "Skip taxonomy" (four stable labels plus the
// residual narrow-quirk bucket). w3cSkipClassUnclassified is NOT a taxonomy
// label; it is the sentinel the drift-check treats as a possible mandatory
// Basic-XSLT-3.0 regression.
const (
	w3cClassPerformanceGated   = "performance-gated"
	w3cClassExpectedDivergence = "expected-divergence"
	w3cClassNotClaimed         = "not-claimed"
	w3cClassDeliberatelyDenied = "deliberately-denied"
	w3cClassNarrowQuirk        = "narrow-quirk"
	w3cClassUnclassified       = "unclassified"
)

// w3cNarrowQuirkIDs is the closed, enumerated narrow-quirk allowlist from
// CONFORMANCE.md's "Narrow-quirk list". Membership is by test id, not by reason
// text, so a novel skip can never silently land in this bucket: it must match
// one of the four taxonomy reason patterns below or it is flagged unclassified.
var w3cNarrowQuirkIDs = map[string]struct{}{
	"validation-0201":          {},
	"regex-syntax-xslt20-0984": {},
	"backwards-041":            {},
	"backwards-019":            {},
	"import-schema-203":        {},
	"normalize-unicode-008":    {},
	"error-FODC0002a-ignore":   {},
	"error-1160a":              {},
	"assert-007":               {},
	"format-number-070":        {},
	"select-3401":              {},
	"docbook-001":              {},
	"docbook-002":              {},
}

// w3cSkipDecision replays, as a pure function, the ordered skip checks in
// w3cRunOne. It returns whether tc is skipped in a run and the exact reason
// string t.Skip would carry. slowEnabled mirrors HELIUM_SLOW_TESTS being set.
// It is the single source of truth for both the live runner and the ledger.
func w3cSkipDecision(tc w3cTest, slowEnabled bool) (bool, string) {
	if _, slow := w3cSlowTests[tc.Name]; slow && !slowEnabled {
		return true, "slow test; set HELIUM_SLOW_TESTS=1 to run"
	}
	if isSlowSourceDoc(tc.SourceDocPath) && !slowEnabled {
		return true, "slow source doc; set HELIUM_SLOW_TESTS=1 to run"
	}
	if isSlowStreamingTest(tc.Name) && !slowEnabled {
		return true, "slow streaming test (big-transactions.xml); set HELIUM_SLOW_TESTS=1 to run"
	}
	if reason := w3cImplicitSkipReason(tc.Name); reason != "" {
		return true, reason
	}
	if tc.Skip != "" {
		return true, tc.Skip
	}
	if tc.StylesheetPath == "" && !tc.EmbeddedStylesheet {
		return true, "no stylesheet"
	}
	return false, ""
}

// w3cSkipClass maps a (test id, reason) to its CONFORMANCE.md taxonomy label.
// Narrow-quirk is decided first, by the closed id allowlist. The remaining four
// labels are decided by reason-string patterns that mirror CONFORMANCE.md's
// "Reconciliation to the 781 default-run skips" composition. Anything matching
// none returns w3cClassUnclassified — the proxy for "a mandatory Basic 3.0
// facility became skipped" (see drift-check condition (d)).
func w3cSkipClass(name, reason string) string {
	if _, ok := w3cNarrowQuirkIDs[name]; ok {
		return w3cClassNarrowQuirk
	}
	switch {
	case strings.Contains(reason, "HELIUM_SLOW_TESTS=1"),
		strings.HasPrefix(reason, "too slow for CI"):
		return w3cClassPerformanceGated
	case strings.Contains(reason, "network access"),
		strings.Contains(reason, "external entity resolution"),
		strings.Contains(reason, "external parameter entity resolution"):
		return w3cClassDeliberatelyDenied
	case strings.HasPrefix(reason, "feature present but test requires absent:"),
		strings.HasPrefix(reason, "year component value present but test requires absent:"),
		strings.Contains(reason, "load-xquery-module"),
		strings.Contains(reason, "uri-collection Saxon-format"):
		return w3cClassNotClaimed
	case strings.HasPrefix(reason, "XSLT 2.0-only:"),
		strings.HasPrefix(reason, "legitimate 2.0-vs-3.0 divergence:"),
		strings.HasPrefix(reason, "XSLT 2.0 test"),
		strings.Contains(reason, "implementation handles zero-length matches"),
		strings.Contains(reason, "XSD 1.0-only regex error"),
		strings.Contains(reason, "XSD 1.0 test; our processor targets XSD 1.1"):
		return w3cClassExpectedDivergence
	default:
		return w3cClassUnclassified
	}
}

// w3cSpecDependency derives the best-effort spec / feature dependency the skip
// hinges on, for the ledger's spec-dependency column. Empty where not
// applicable (e.g. a pure performance gate).
func w3cSpecDependency(class, reason string) string {
	switch class {
	case w3cClassExpectedDivergence:
		if strings.Contains(reason, "XSD 1.0") {
			return "XSD 1.0"
		}
		return "XSLT20"
	case w3cClassNotClaimed:
		if f := strings.TrimPrefix(reason, "feature present but test requires absent: "); f != reason {
			return f
		}
		if strings.HasPrefix(reason, "year component value present but test requires absent:") {
			return "year-component-out-of-range"
		}
		if strings.Contains(reason, "load-xquery-module") {
			return "load-xquery-module"
		}
		if strings.Contains(reason, "uri-collection Saxon-format") {
			return "uri-collection-glob"
		}
	case w3cClassNarrowQuirk:
		if strings.Contains(reason, "XPath 1.0 grammar") {
			return "XPath10-grammar"
		}
	}
	return ""
}

// w3cPairedReasonPatterns extract a paired passing 3.0 case named in the reason
// text. Order matters: the first match wins.
var w3cPairedReasonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`paired ([A-Za-z0-9_-]+) PASSes`),
	regexp.MustCompile(`XSLT30\+ variant ([A-Za-z0-9_-]+)`),
	regexp.MustCompile(`passing 3\.0 variant ([A-Za-z0-9_-]+)`),
	regexp.MustCompile(`byte-identical to the passing ([A-Za-z0-9_-]+)`),
}

var w3cRegexXSLT20Name = regexp.MustCompile(`^regex-syntax-xslt20-(.+)$`)

// w3cPairedPassingCase returns the id of the passing XSLT 3.0 companion this
// skip is paired with, where derivable. The regex-syntax-xslt20-NNNN naming
// convention maps directly to regex-syntax-NNNN; otherwise the reason text is
// mined for an explicitly named paired case. Empty when none is stated.
func w3cPairedPassingCase(name, reason string) string {
	if m := w3cRegexXSLT20Name.FindStringSubmatch(name); m != nil {
		return "regex-syntax-" + m[1]
	}
	for _, re := range w3cPairedReasonPatterns {
		if m := re.FindStringSubmatch(reason); m != nil {
			return m[1]
		}
	}
	return ""
}

// w3cSkipLedgerRow is one ledger entry: a single skipped test's contract.
type w3cSkipLedgerRow struct {
	TestID              string `json:"test_id"`
	UpstreamSuiteCommit string `json:"upstream_suite_commit"`
	SkipClass           string `json:"skip_class"`
	Reason              string `json:"reason"`
	SpecDependency      string `json:"spec_dependency"`
	PairedPassing30Case string `json:"paired_passing_3_0_case"`
	IssueLink           string `json:"issue_link"`
}

// w3cSkipLedger is the top-level ledger document.
type w3cSkipLedger struct {
	Suite               string             `json:"suite"`
	UpstreamSuiteCommit string             `json:"upstream_suite_commit"`
	GeneratedBy         string             `json:"generated_by"`
	Note                string             `json:"note"`
	Rows                []w3cSkipLedgerRow `json:"rows"`
}

// w3cRunCounts is the skip breakdown for one run mode.
type w3cRunCounts struct {
	Skipped int            `json:"skipped"`
	ByClass map[string]int `json:"by_class"`
}

// w3cSkipCounts is the committed count contract (drift-check condition (e)).
type w3cSkipCounts struct {
	Suite               string       `json:"suite"`
	UpstreamSuiteCommit string       `json:"upstream_suite_commit"`
	TotalCases          int          `json:"total_cases"`
	ExpectedFail        int          `json:"expected_fail"`
	DefaultRun          w3cRunCounts `json:"default_run"`
	SlowRun             w3cRunCounts `json:"slow_run"`
}

// w3cBuildSkipLedger builds the default-run ledger rows (sorted by test id) and
// the count contract (default and slow runs). Deterministic: no map iteration
// order leaks into output.
func w3cBuildSkipLedger() (w3cSkipLedger, w3cSkipCounts) {
	rows := make([]w3cSkipLedgerRow, 0, 1024)
	defaultByClass := map[string]int{}
	slowByClass := map[string]int{}
	defaultSkipped := 0
	slowSkipped := 0

	for _, tc := range xslt30AllCases {
		if skip, reason := w3cSkipDecision(tc, true); skip {
			slowSkipped++
			slowByClass[w3cSkipClass(tc.Name, reason)]++
		}
		skip, reason := w3cSkipDecision(tc, false)
		if !skip {
			continue
		}
		defaultSkipped++
		class := w3cSkipClass(tc.Name, reason)
		defaultByClass[class]++
		rows = append(rows, w3cSkipLedgerRow{
			TestID:              tc.Name,
			UpstreamSuiteCommit: w3cPinnedCommit,
			SkipClass:           class,
			Reason:              reason,
			SpecDependency:      w3cSpecDependency(class, reason),
			PairedPassing30Case: w3cPairedPassingCase(tc.Name, reason),
			IssueLink:           "",
		})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].TestID < rows[j].TestID })

	ledger := w3cSkipLedger{
		Suite:               w3cSuiteName,
		UpstreamSuiteCommit: w3cPinnedCommit,
		GeneratedBy:         "go test ./xslt3 -run TestXSLT30SkipLedger -update-ledger",
		Note: "machine-generated conformance skip contract; do not hand-edit. " +
			"Regenerate from the skip sources with the command in generated_by. " +
			"Skip-class labels are defined in helium xslt3/CONFORMANCE.md.",
		Rows: rows,
	}
	counts := w3cSkipCounts{
		Suite:               w3cSuiteName,
		UpstreamSuiteCommit: w3cPinnedCommit,
		TotalCases:          len(xslt30AllCases),
		ExpectedFail:        w3cExpectedFail,
		DefaultRun:          w3cRunCounts{Skipped: defaultSkipped, ByClass: defaultByClass},
		SlowRun:             w3cRunCounts{Skipped: slowSkipped, ByClass: slowByClass},
	}
	return ledger, counts
}

func w3cMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

// TestXSLT30SkipLedger regenerates or verifies the checked-in skip ledger.
//
// With -update-ledger it writes the ledger and count contract. Without it, it
// is the fixture-free CI drift-check and FAILS on any of five conditions:
//
//	(a) the committed count contract tolerates a nonzero failure count, or an
//	    xfail expectation is declared (a failure is being green-listed);
//	(b) a currently-skipped test is absent from the committed ledger (a new,
//	    unrecorded skip);
//	(c) a committed ledger row is no longer skipped, or its reason / skip-class
//	    (or any other column) changed versus the regenerated row;
//	(d) any current skip classifies as "unclassified" — the conservative proxy
//	    for a mandatory Basic-XSLT-3.0 facility becoming skipped. Because the
//	    generated case tables no longer carry the upstream <dependency> element,
//	    "mandatory" cannot be read directly; instead every legitimate skip must
//	    land in one of the four taxonomy labels or the closed narrow-quirk
//	    allowlist. A skip whose reason matches none of those patterns is treated
//	    as a potential mandatory-feature regression and fails the build,
//	    forcing a human to classify it (and confirm it is not mandatory) before
//	    the ledger can be regenerated;
//	(e) the default-run or slow-run skip count (or per-class breakdown, or total
//	    case count) changed versus the committed count contract.
func TestXSLT30SkipLedger(t *testing.T) {
	ledger, counts := w3cBuildSkipLedger()
	ledgerJSON := w3cMarshalJSON(t, ledger)
	countsJSON := w3cMarshalJSON(t, counts)

	if *updateLedger {
		if err := os.WriteFile(filepath.FromSlash(w3cSkipLedgerPath), ledgerJSON, 0o644); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
		if err := os.WriteFile(filepath.FromSlash(w3cSkipCountsPath), countsJSON, 0o644); err != nil {
			t.Fatalf("write counts: %v", err)
		}
		t.Logf("wrote %s (%d rows) and %s", w3cSkipLedgerPath, len(ledger.Rows), w3cSkipCountsPath)
		return
	}

	committedLedger := w3cReadLedger(t)
	committedCounts := w3cReadCounts(t)

	// Condition (d): mandatory-feature guard. Independent of the committed
	// files — recompute every current skip's class and reject "unclassified".
	for _, r := range ledger.Rows {
		if r.SkipClass == w3cClassUnclassified {
			t.Errorf("drift (d) unclassified skip %q: reason %q does not match any taxonomy label; "+
				"classify it in w3cSkipClass / CONFORMANCE.md (or confirm it is not a mandatory Basic 3.0 feature)",
				r.TestID, r.Reason)
		}
	}

	// Condition (a): zero-failure contract. Actual pass/fail is proven by the
	// slow suite job; here the committed count contract must assert zero
	// failures and no case may be xfail-listed.
	if committedCounts.ExpectedFail != 0 {
		t.Errorf("drift (a): committed count contract tolerates %d failures, must be 0", committedCounts.ExpectedFail)
	}
	if xf := w3cExpectationsXFail(); len(xf) != 0 {
		t.Errorf("drift (a): %d xfail expectation(s) declared in expectations/xslt30.json; a green-listed failure is not allowed: %v",
			len(xf), w3cSortedKeys(xf))
	}

	// Index committed rows by id for conditions (b) and (c).
	committedByID := make(map[string]w3cSkipLedgerRow, len(committedLedger.Rows))
	for _, r := range committedLedger.Rows {
		committedByID[r.TestID] = r
	}
	currentByID := make(map[string]w3cSkipLedgerRow, len(ledger.Rows))
	for _, r := range ledger.Rows {
		currentByID[r.TestID] = r
	}

	// Condition (b): a current skip missing from the committed ledger.
	for _, r := range ledger.Rows {
		if _, ok := committedByID[r.TestID]; !ok {
			t.Errorf("drift (b) new skip not in ledger: %q (class %s, reason %q); "+
				"regenerate with -update-ledger after confirming the skip is intended",
				r.TestID, r.SkipClass, r.Reason)
		}
	}

	// Condition (c): a committed row no longer skipped, or any column changed.
	for _, want := range committedLedger.Rows {
		got, ok := currentByID[want.TestID]
		if !ok {
			t.Errorf("drift (c) ledger id no longer skipped: %q; regenerate with -update-ledger", want.TestID)
			continue
		}
		switch {
		case got.SkipClass != want.SkipClass:
			t.Errorf("drift (c) skip-class changed for %q: committed %s, now %s", want.TestID, want.SkipClass, got.SkipClass)
		case got.Reason != want.Reason:
			t.Errorf("drift (c) reason changed for %q without updating the ledger:\n  committed: %q\n  now:       %q",
				want.TestID, want.Reason, got.Reason)
		case got != want:
			t.Errorf("drift (c) ledger row changed for %q (spec-dependency / paired-case / commit / issue-link); regenerate with -update-ledger", want.TestID)
		}
	}

	// Condition (e): skip counts / breakdown changed.
	if counts.TotalCases != committedCounts.TotalCases {
		t.Errorf("drift (e) total case count changed: committed %d, now %d", committedCounts.TotalCases, counts.TotalCases)
	}
	if counts.DefaultRun.Skipped != committedCounts.DefaultRun.Skipped {
		t.Errorf("drift (e) default-run skip count changed: committed %d, now %d",
			committedCounts.DefaultRun.Skipped, counts.DefaultRun.Skipped)
	}
	if counts.SlowRun.Skipped != committedCounts.SlowRun.Skipped {
		t.Errorf("drift (e) slow-run skip count changed: committed %d, now %d",
			committedCounts.SlowRun.Skipped, counts.SlowRun.Skipped)
	}
	w3cDiffClassCounts(t, "default", committedCounts.DefaultRun.ByClass, counts.DefaultRun.ByClass)
	w3cDiffClassCounts(t, "slow", committedCounts.SlowRun.ByClass, counts.SlowRun.ByClass)

	// Catch-all: the committed files must be byte-identical to a regeneration,
	// so nothing (formatting, ordering, a stray field) can drift unnoticed.
	if committed := w3cReadFile(t, w3cSkipLedgerPath); string(committed) != string(ledgerJSON) {
		t.Errorf("drift: %s is not byte-identical to a regeneration; run: go test ./xslt3 -run TestXSLT30SkipLedger -update-ledger", w3cSkipLedgerPath)
	}
	if committed := w3cReadFile(t, w3cSkipCountsPath); string(committed) != string(countsJSON) {
		t.Errorf("drift: %s is not byte-identical to a regeneration; run: go test ./xslt3 -run TestXSLT30SkipLedger -update-ledger", w3cSkipCountsPath)
	}
}

func w3cDiffClassCounts(t *testing.T, run string, want, got map[string]int) {
	t.Helper()
	keys := map[string]struct{}{}
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	for _, k := range w3cSortedKeys(keys) {
		if want[k] != got[k] {
			t.Errorf("drift (e) %s-run %s count changed: committed %d, now %d", run, k, want[k], got[k])
		}
	}
}

func w3cReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with: go test ./xslt3 -run TestXSLT30SkipLedger -update-ledger)", path, err)
	}
	return b
}

func w3cReadLedger(t *testing.T) w3cSkipLedger {
	t.Helper()
	var l w3cSkipLedger
	if err := json.Unmarshal(w3cReadFile(t, w3cSkipLedgerPath), &l); err != nil {
		t.Fatalf("parse committed ledger: %v", err)
	}
	return l
}

func w3cReadCounts(t *testing.T) w3cSkipCounts {
	t.Helper()
	var c w3cSkipCounts
	if err := json.Unmarshal(w3cReadFile(t, w3cSkipCountsPath), &c); err != nil {
		t.Fatalf("parse committed counts: %v", err)
	}
	return c
}

// w3cExpectationsXFail returns the xfail map from expectations/xslt30.json.
func w3cExpectationsXFail() map[string]string {
	w3cExpectationSkips() // ensure the file is loaded exactly once
	return w3cExpectationsData.XFail
}

func w3cSortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
