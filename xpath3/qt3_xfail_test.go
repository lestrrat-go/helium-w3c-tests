package xpath3_test

import (
	"testing"

	"github.com/lestrrat-go/helium/xpath3"
)

// TestQT3XFailMechanism locks the xfail recorder used by qt3RunXFail. The live
// expectations/qt3.json xfail map is empty, so without this the unexpected-pass
// tripwire would ship unexercised. qt3CheckResult must route a failing
// assertion to a recorder failure (so an xfail-listed case counts as the
// expected divergence) and leave a passing case unmarked (so qt3RunXFail flags
// the unexpected pass and forces the entry's removal).
func TestQT3XFailMechanism(t *testing.T) {
	ctx := t.Context()
	run := func(tc qt3Test) bool {
		rec := &qt3Recorder{ctx: ctx}
		done := make(chan struct{})
		go func() {
			defer close(done)
			qt3CheckResult(rec, ctx, tc, xpath3.NewEvaluator(xpath3.DefaultEvaluatorOptions), nil)
		}()
		<-done
		return rec.failed
	}

	if run(qt3Test{Name: "pass", XPath: "1 eq 1", Assertions: []qt3Assertion{qt3AssertTrue()}}) {
		t.Errorf("passing case recorded a failure; xfail tripwire would miss an unexpected pass")
	}
	if !run(qt3Test{Name: "fail", XPath: "1 eq 2", Assertions: []qt3Assertion{qt3AssertTrue()}}) {
		t.Errorf("failing case did not record a failure; xfail would report a false unexpected pass")
	}
}

// TestQT3ExpectationsValid fails on a hand-authored expectations/qt3.json entry
// that names no real case, or an xfail that is force-skipped before qt3RunXFail
// (so its unexpected-pass tripwire could never fire). This keeps a typo'd or
// stale key from silently green-listing nothing.
func TestQT3ExpectationsValid(t *testing.T) {
	for _, p := range qt3ValidateExpectations(qt3LoadExpectations(), qt3AllCases) {
		t.Errorf("expectations/qt3.json: %s", p)
	}
}
