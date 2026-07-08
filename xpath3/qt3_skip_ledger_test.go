package xpath3_test

// This file is the machine-readable, CI-enforced conformance skip contract for
// the W3C QT3 (XPath 3.1) suite. The dependency-derived skips are produced by the
// generator (internal/suites/qt3/gen.go) and materialized as the Skip field on
// every generated qt3Test; this test turns them into an auditable, committed,
// drift-checked artifact so that closing a skip category (wiring an adapter,
// fixing a divergence) shows up as an explicit, reviewable delta.
//
//	1. qt3SkipClass — the pure reason->taxonomy classifier. Every current skip
//	   reason must land in a real class; a novel reason falls through to
//	   "unclassified" and fails the drift-check, forcing a human to classify it
//	   (the proxy for "a mandatory XPath 3.1 facility silently became skipped").
//	2. TestQT3SkipLedger with -update-ledger regenerates the checked-in ledger
//	   (expectations/qt3-skip-ledger.json) and count contract
//	   (expectations/qt3-skip-counts.json) from qt3AllCases.
//	3. TestQT3SkipLedger without the flag is the fast, fixture-free CI
//	   drift-check: it regenerates in memory and fails on any drift.
//
// Regenerate after an intentional skip change:
//
//	go test ./xpath3 -run TestQT3SkipLedger -update-ledger
//
//go:generate go test . -run TestQT3SkipLedger -update-ledger

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// containsAny reports whether s contains any of subs.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

var qt3UpdateLedger = flag.Bool("update-ledger", false,
	"regenerate the checked-in QT3 skip ledger and count contract")

const (
	qt3SkipLedgerPath = "../expectations/qt3-skip-ledger.json"
	qt3SkipCountsPath = "../expectations/qt3-skip-counts.json"
)

// Skip-class labels. A dependency-derived skip lands in exactly one; the
// campaign drains not-wired / standalone-limitation / helium-divergence toward
// zero as adapters are wired and divergences fixed, leaving only out-of-scope.
// qt3ClassUnclassified is the sentinel the drift-check rejects.
const (
	// out-of-scope: not required for the target conformance level, or an
	// environment concern — a legitimate permanent skip. XQuery module/library
	// loading, XSD 1.0-only behavior (xpath3 targets XSD 1.1), XPath 2.0-only
	// behavior, XML 1.1, remote HTTP, an unsupported Unicode version, and the
	// optional Static Typing Feature (a dynamically-typed processor need not
	// raise the compile-time XPTY0004/XPST0005 these cases expect).
	qt3ClassOutOfScope = "out-of-scope"
	// not-wired: helium has the capability in another package but it is not
	// wired into the standalone qt3 evaluator path yet (fn:transform via xslt3,
	// XML Schema support, directory/stability collections). Closeable.
	qt3ClassNotWired = "not-wired"
	// standalone-limitation: the standalone xpath3 evaluator lacks a
	// schema/PSVI property a schema-aware path would supply (PSVI
	// nilled/is-id/whitespace-stripping construction, json-to-xml validate).
	qt3ClassStandaloneLimit = "standalone-limitation"
	// helium-divergence: helium supports the feature but returns a
	// non-conformant result (a missing error code). A genuine bug; fix it.
	qt3ClassHeliumDivergence = "helium-divergence"
	// unclassified: reason matches no taxonomy label — treated as a possible
	// mandatory-feature regression and fails the drift-check.
	qt3ClassUnclassified = "unclassified"
)

// qt3SkipClass maps a dependency-derived skip reason to its taxonomy label. The
// reason strings are produced by internal/suites/qt3/gen.go (getSkipReason,
// getTestCaseSkipReason, checkEnvironmentSupport, schemaAwareSkip). Order
// matters: the most specific signals (error codes, then standalone-evaluator /
// PSVI) are tested before the coarser scope buckets (which include the optional
// static-typing feature).
func qt3SkipClass(reason string) string {
	switch {
	case containsAny(reason, "SENR0001", "XPTY0004", "FOTY0012"):
		return qt3ClassHeliumDivergence
	case containsAny(reason, "PSVI", "standalone evaluator"):
		return qt3ClassStandaloneLimit
	case containsAny(reason, "static typing", "XQuery", "XSD 1.0", "XML 1.1", "XPath 2.0", "remote HTTP"),
		strings.HasPrefix(reason, "requires Unicode "):
		return qt3ClassOutOfScope
	case containsAny(reason, "XSLT transform", "fn:transform", "XML Schema support", "collection"),
		strings.HasPrefix(reason, "requires source role "):
		return qt3ClassNotWired
	default:
		return qt3ClassUnclassified
	}
}

// qt3ValidateExpectations checks that every hand-authored skip/xfail entry in
// expectations/qt3.json refers to a real case and, for xfails, one that actually
// runs — so a typo or stale key can't silently disable the unexpected-pass
// tripwire (a green-listed divergence would then never be re-checked). It
// returns one message per problem, sorted for deterministic output:
//   - a skip/xfail id that matches no generated case;
//   - an xfail id also present in the skip map, or one whose generated case
//     carries a dependency-derived Skip — either way the case is skipped before
//     qt3RunXFail, so its tripwire never runs.
func qt3ValidateExpectations(exp qt3Expectations, cases []qt3Test) []string {
	known := make(map[string]struct{}, len(cases))
	genSkip := make(map[string]string, len(cases))
	for _, tc := range cases {
		known[tc.Name] = struct{}{}
		if tc.Skip != "" {
			genSkip[tc.Name] = tc.Skip
		}
	}
	var problems []string
	for id := range exp.Skip {
		if _, ok := known[id]; !ok {
			problems = append(problems, fmt.Sprintf("skip id %q matches no QT3 case", id))
		}
	}
	for id := range exp.XFail {
		if _, ok := known[id]; !ok {
			problems = append(problems, fmt.Sprintf("xfail id %q matches no QT3 case", id))
			continue
		}
		if _, ok := exp.Skip[id]; ok {
			problems = append(problems, fmt.Sprintf("xfail id %q is also in the skip map; it would be skipped before the xfail tripwire runs", id))
		}
		if r, ok := genSkip[id]; ok {
			problems = append(problems, fmt.Sprintf("xfail id %q has a dependency-derived skip (%q); it would be skipped before the xfail tripwire runs", id, r))
		}
	}
	sort.Strings(problems)
	return problems
}

// qt3SkipLedgerRow is one ledger entry: a single skipped case's contract.
type qt3SkipLedgerRow struct {
	TestID              string `json:"test_id"`
	UpstreamSuiteCommit string `json:"upstream_suite_commit"`
	SkipClass           string `json:"skip_class"`
	Reason              string `json:"reason"`
}

// qt3SkipLedger is the top-level ledger document.
type qt3SkipLedger struct {
	Suite               string             `json:"suite"`
	UpstreamSuiteCommit string             `json:"upstream_suite_commit"`
	GeneratedBy         string             `json:"generated_by"`
	Note                string             `json:"note"`
	Rows                []qt3SkipLedgerRow `json:"rows"`
}

// qt3SkipCounts is the committed count contract. ExpectedFail tracks the number
// of hand-authored xfail entries in expectations/qt3.json (cases that run but
// are expected to diverge), so adding or removing one forces an auditable
// regeneration.
type qt3SkipCounts struct {
	Suite               string `json:"suite"`
	UpstreamSuiteCommit string `json:"upstream_suite_commit"`
	TotalCases          int    `json:"total_cases"`
	ExpectedFail        int    `json:"expected_fail"`
	Skipped             int    `json:"skipped"`
	// FalsePassRisk is the number of RUN (non-skipped) cases that carry no real
	// assertion and expect no error — green no-ops that verify only "evaluation
	// did not error". Locked in the committed contract so un-skipping a weak-only
	// case (or regressing a real assertion to a no-op) forces an auditable
	// regeneration. See TestQT3WeakNoOpGuard.
	FalsePassRisk int            `json:"false_pass_risk"`
	ByClass       map[string]int `json:"by_class"`
}

// qt3BuildSkipLedger builds the ledger rows (sorted by test id) and the count
// contract from qt3AllCases. Deterministic: no map iteration order leaks into
// output. It fatals on a duplicate skipped-case id, the invariant the ledger key
// relies on (case names are set-local upstream but globally unique among skips).
func qt3BuildSkipLedger(t *testing.T) (qt3SkipLedger, qt3SkipCounts) {
	t.Helper()
	rows := make([]qt3SkipLedgerRow, 0, 512)
	byClass := map[string]int{}
	seen := map[string]struct{}{}
	skipped := 0
	falsePassRisk := 0

	for _, tc := range qt3AllCases {
		if tc.Skip == "" {
			if tc.FalsePassRisk {
				falsePassRisk++
			}
			continue
		}
		if _, dup := seen[tc.Name]; dup {
			t.Fatalf("duplicate skipped case id %q: the ledger keys by case name, which must be unique among skips", tc.Name)
		}
		seen[tc.Name] = struct{}{}
		skipped++
		class := qt3SkipClass(tc.Skip)
		byClass[class]++
		rows = append(rows, qt3SkipLedgerRow{
			TestID:              tc.Name,
			UpstreamSuiteCommit: w3cPinnedCommit,
			SkipClass:           class,
			Reason:              tc.Skip,
		})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].TestID < rows[j].TestID })

	ledger := qt3SkipLedger{
		Suite:               w3cSuiteName,
		UpstreamSuiteCommit: w3cPinnedCommit,
		GeneratedBy:         "go test ./xpath3 -run TestQT3SkipLedger -update-ledger",
		Note: "machine-generated conformance skip contract; do not hand-edit. " +
			"Skips are dependency-derived in internal/suites/qt3/gen.go and materialized " +
			"onto each generated qt3Test. Regenerate from the sources with the command in generated_by.",
		Rows: rows,
	}
	counts := qt3SkipCounts{
		Suite:               w3cSuiteName,
		UpstreamSuiteCommit: w3cPinnedCommit,
		TotalCases:          len(qt3AllCases),
		ExpectedFail:        len(qt3LoadExpectations().XFail),
		Skipped:             skipped,
		FalsePassRisk:       falsePassRisk,
		ByClass:             byClass,
	}
	return ledger, counts
}

func qt3MarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

// TestQT3SkipLedger regenerates or verifies the checked-in skip ledger.
//
// With -update-ledger it writes the ledger and count contract. Without it, it is
// the fixture-free CI drift-check and FAILS on any of:
//
//	(a) any current skip classifies as "unclassified" — the proxy for a
//	    mandatory XPath 3.1 facility silently becoming skipped;
//	(b) a current skip is absent from the committed ledger (a new, unrecorded
//	    skip);
//	(c) a committed row is no longer skipped, or any column changed;
//	(d) the skip count / per-class breakdown / total case count / expected-fail
//	    count changed versus the committed count contract;
//	(e) the committed files are not byte-identical to a regeneration.
func TestQT3SkipLedger(t *testing.T) {
	ledger, counts := qt3BuildSkipLedger(t)
	ledgerJSON := qt3MarshalJSON(t, ledger)
	countsJSON := qt3MarshalJSON(t, counts)

	if *qt3UpdateLedger {
		if err := os.WriteFile(filepath.FromSlash(qt3SkipLedgerPath), ledgerJSON, 0o644); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
		if err := os.WriteFile(filepath.FromSlash(qt3SkipCountsPath), countsJSON, 0o644); err != nil {
			t.Fatalf("write counts: %v", err)
		}
		t.Logf("wrote %s (%d rows) and %s", qt3SkipLedgerPath, len(ledger.Rows), qt3SkipCountsPath)
		return
	}

	committedLedger := qt3ReadLedger(t)
	committedCounts := qt3ReadCounts(t)

	// Expectation hygiene: a hand-authored skip/xfail entry that names no real
	// (or, for xfail, no actually-running) case would silently disable the
	// unexpected-pass tripwire. Enforce it here so the fixture-free ledger CI job
	// catches a typo'd or stale entry.
	for _, p := range qt3ValidateExpectations(qt3LoadExpectations(), qt3AllCases) {
		t.Errorf("expectations/qt3.json: %s", p)
	}

	// (a) mandatory-feature guard, independent of the committed files.
	for _, r := range ledger.Rows {
		if r.SkipClass == qt3ClassUnclassified {
			t.Errorf("drift (a) unclassified skip %q: reason %q matches no taxonomy label; "+
				"classify it in qt3SkipClass (or confirm it is not a mandatory XPath 3.1 feature)",
				r.TestID, r.Reason)
		}
	}

	committedByID := make(map[string]qt3SkipLedgerRow, len(committedLedger.Rows))
	for _, r := range committedLedger.Rows {
		committedByID[r.TestID] = r
	}
	currentByID := make(map[string]qt3SkipLedgerRow, len(ledger.Rows))
	for _, r := range ledger.Rows {
		currentByID[r.TestID] = r
	}

	// (b) a current skip missing from the committed ledger.
	for _, r := range ledger.Rows {
		if _, ok := committedByID[r.TestID]; !ok {
			t.Errorf("drift (b) new skip not in ledger: %q (class %s, reason %q); "+
				"regenerate with -update-ledger after confirming the skip is intended",
				r.TestID, r.SkipClass, r.Reason)
		}
	}

	// (c) a committed row no longer skipped, or any column changed.
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
			t.Errorf("drift (c) ledger row changed for %q (upstream commit); regenerate with -update-ledger", want.TestID)
		}
	}

	// (d) counts / breakdown changed.
	if counts.TotalCases != committedCounts.TotalCases {
		t.Errorf("drift (d) total case count changed: committed %d, now %d", committedCounts.TotalCases, counts.TotalCases)
	}
	if counts.Skipped != committedCounts.Skipped {
		t.Errorf("drift (d) skip count changed: committed %d, now %d", committedCounts.Skipped, counts.Skipped)
	}
	if counts.ExpectedFail != committedCounts.ExpectedFail {
		t.Errorf("drift (d) expected-fail (xfail) count changed: committed %d, now %d", committedCounts.ExpectedFail, counts.ExpectedFail)
	}
	if counts.FalsePassRisk != committedCounts.FalsePassRisk {
		t.Errorf("drift (d) false-pass-risk (weak-only RUN) count changed: committed %d, now %d; "+
			"un-skipping a weak-only case or regressing a real assertion to a no-op requires -update-ledger and review",
			committedCounts.FalsePassRisk, counts.FalsePassRisk)
	}
	qt3DiffClassCounts(t, committedCounts.ByClass, counts.ByClass)

	// (e) byte-identical catch-all.
	if committed := qt3ReadFile(t, qt3SkipLedgerPath); string(committed) != string(ledgerJSON) {
		t.Errorf("drift (e) %s is not byte-identical to a regeneration; run: go test ./xpath3 -run TestQT3SkipLedger -update-ledger", qt3SkipLedgerPath)
	}
	if committed := qt3ReadFile(t, qt3SkipCountsPath); string(committed) != string(countsJSON) {
		t.Errorf("drift (e) %s is not byte-identical to a regeneration; run: go test ./xpath3 -run TestQT3SkipLedger -update-ledger", qt3SkipCountsPath)
	}
}

// TestQT3WeakNoOpGuard is the dedicated un-skip guard: it counts RUN
// (non-skipped) cases that carry no real assertion and expect no error — green
// no-ops that verify only "evaluation did not error" — and fails if that count
// drifts from the committed contract (expectations/qt3-skip-counts.json).
//
// This catches the specific regression the phase-3 assert-xml/permutation work
// guards against: un-skipping a case whose assertions are ALL weak/no-op
// (assert-serialization, an unimplemented form, a weak any-of branch) silently
// turns it into a passing test that can never fail on a wrong result. Removing
// such a case from the skip set increments the weak-only count and fails here,
// forcing the author to either add a real assertion (or expected error) or keep
// the case skipped. An intentional change is recorded with -update-ledger.
func TestQT3WeakNoOpGuard(t *testing.T) {
	weak := 0
	for _, tc := range qt3AllCases {
		if tc.Skip != "" {
			continue
		}
		if tc.FalsePassRisk {
			weak++
		}
	}
	committed := qt3ReadCounts(t).FalsePassRisk
	if weak != committed {
		t.Errorf("weak-only RUN (green no-op) count is %d, committed %d: a RUN case verifies only "+
			"that evaluation did not error (no real assertion, no expected error). If a weak-only case "+
			"was un-skipped, add a real assertion / expected error or keep it skipped; if the change is "+
			"intended, regenerate: go test ./xpath3 -run TestQT3SkipLedger -update-ledger", weak, committed)
	}
}

func qt3DiffClassCounts(t *testing.T, want, got map[string]int) {
	t.Helper()
	keys := map[string]struct{}{}
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if want[k] != got[k] {
			t.Errorf("drift (d) %s count changed: committed %d, now %d", k, want[k], got[k])
		}
	}
}

func qt3ReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with: go test ./xpath3 -run TestQT3SkipLedger -update-ledger)", path, err)
	}
	return b
}

func qt3ReadLedger(t *testing.T) qt3SkipLedger {
	t.Helper()
	var l qt3SkipLedger
	if err := json.Unmarshal(qt3ReadFile(t, qt3SkipLedgerPath), &l); err != nil {
		t.Fatalf("parse committed ledger: %v", err)
	}
	return l
}

func qt3ReadCounts(t *testing.T) qt3SkipCounts {
	t.Helper()
	var c qt3SkipCounts
	if err := json.Unmarshal(qt3ReadFile(t, qt3SkipCountsPath), &c); err != nil {
		t.Fatalf("parse committed counts: %v", err)
	}
	return c
}
