package xpath3_test

import (
	"testing"

	"github.com/lestrrat-go/helium/xpath3"
	"github.com/stretchr/testify/require"
)

// TestQT3PermutationTypeAware locks the type-aware atomic equality that
// assert-permutation (and assert-deep-eq) rely on: an item with the right lexical
// form but the WRONG type must NOT be accepted as a permutation member —
// xs:string("1993-03-31") is not deep-equal to xs:date("1993-03-31") — while a
// genuinely reordered typed sequence still is. Without this, qt3IsPermutation
// would false-pass a result whose typed values are wrong (e.g.
// fn-unordered-mix-args-021, which asserts typed date/time/dateTime values).
func TestQT3PermutationTypeAware(t *testing.T) {
	eval := func(expr string) xpath3.Sequence {
		s, err := qt3EvalExprSeq(expr, nil, nil)
		require.NoError(t, err, "eval %s", expr)
		return s
	}

	strSeq := eval(`("1993-03-31", "2000-01-01")`)
	dateSeq := eval(`(xs:date("1993-03-31"), xs:date("2000-01-01"))`)
	require.False(t, qt3IsPermutation(strSeq, dateSeq),
		"xs:string must not be a permutation member of xs:date with the same lexical form")
	require.False(t, qt3IsPermutation(dateSeq, strSeq),
		"xs:date must not be a permutation member of xs:string with the same lexical form")

	// A genuinely reordered typed sequence is still a permutation.
	dateSeqPerm := eval(`(xs:date("2000-01-01"), xs:date("1993-03-31"))`)
	require.True(t, qt3IsPermutation(dateSeq, dateSeqPerm),
		"reordered xs:date sequence must be a permutation")

	// A reordered mixed typed sequence is a permutation (also guards the numeric
	// single/double promotion path: xs:float(1.01) eq xs:decimal(1.01)).
	mixed := eval(`("a", xs:float("1.01"), xs:date("1993-03-31"))`)
	mixedPerm := eval(`(xs:date("1993-03-31"), "a", xs:float("1.01"))`)
	require.True(t, qt3IsPermutation(mixed, mixedPerm),
		"reordered mixed typed sequence must be a permutation")

	// A wrong TYPE inside an otherwise-matching mixed sequence must not match.
	mixedWrong := eval(`("a", xs:float("1.01"), "1993-03-31")`)
	require.False(t, qt3IsPermutation(mixed, mixedWrong),
		"a string item must not substitute for the xs:date item")
}

// TestQT3PermutationNodeDeepEqual locks the NODE half: qt3IsPermutation must
// compare nodes by fn:deep-equal (kind + expanded name + attribute set +
// deep-equal children), NOT by string value, so structurally different nodes with
// the same string value are not accepted.
func TestQT3PermutationNodeDeepEqual(t *testing.T) {
	eval := func(expr string) xpath3.Sequence {
		s, err := qt3EvalExprSeq(expr, nil, nil)
		require.NoError(t, err, "eval %s", expr)
		return s
	}

	// Different element NAME, same text value: not a permutation.
	aX := eval(`(parse-xml("<a>x</a>")/*, parse-xml("<c/>")/*)`)
	bX := eval(`(parse-xml("<b>x</b>")/*, parse-xml("<c/>")/*)`)
	require.False(t, qt3IsPermutation(aX, bX),
		"<a>x</a> and <b>x</b> have the same string value but different names — not deep-equal")

	// Different ATTRIBUTE value: not a permutation.
	e1 := eval(`parse-xml('<e a="1"/>')/*`)
	e2 := eval(`parse-xml('<e a="2"/>')/*`)
	require.False(t, qt3IsPermutation(e1, e2),
		"elements differing only in an attribute value are not deep-equal")

	// A genuinely reordered sequence of deep-equal nodes IS a permutation.
	seq := eval(`(parse-xml("<a>x</a>")/*, parse-xml("<c/>")/*)`)
	seqPerm := eval(`(parse-xml("<c/>")/*, parse-xml("<a>x</a>")/*)`)
	require.True(t, qt3IsPermutation(seq, seqPerm),
		"a reordered sequence of deep-equal nodes must be a permutation")

	// An atomic must not match a node with the same string value.
	nodeSeq := eval(`parse-xml("<a>x</a>")/*`)
	atomSeq := eval(`"x"`)
	require.False(t, qt3IsPermutation(nodeSeq, atomSeq),
		"an atomic value must not be a permutation member of a node")
}
