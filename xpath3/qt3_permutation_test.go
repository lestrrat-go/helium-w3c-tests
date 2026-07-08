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
