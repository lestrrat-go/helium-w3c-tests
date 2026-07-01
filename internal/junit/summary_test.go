package junit

import (
	"strings"
	"testing"
)

// go test -json emits one JSON object per line. This fixture covers a pass, two
// skips sharing a reason, one skip with a distinct reason, a fail, the ignored
// root-test event, and an ignored unrelated test.
const sampleJSON = `
{"Action":"run","Test":"TestSuite"}
{"Action":"run","Test":"TestSuite/case-pass"}
{"Action":"output","Test":"TestSuite/case-pass","Output":"=== RUN   TestSuite/case-pass\n"}
{"Action":"pass","Test":"TestSuite/case-pass","Elapsed":0.01}
{"Action":"run","Test":"TestSuite/case-skipA"}
{"Action":"output","Test":"TestSuite/case-skipA","Output":"    helper_test.go:12: unsupported spec: XSLT20\n"}
{"Action":"skip","Test":"TestSuite/case-skipA","Elapsed":0}
{"Action":"run","Test":"TestSuite/case-skipB"}
{"Action":"output","Test":"TestSuite/case-skipB","Output":"    helper_test.go:12: unsupported spec: XSLT20\n"}
{"Action":"skip","Test":"TestSuite/case-skipB","Elapsed":0}
{"Action":"run","Test":"TestSuite/case-skipC"}
{"Action":"output","Test":"TestSuite/case-skipC","Output":"    helper_test.go:20: requires network\n"}
{"Action":"skip","Test":"TestSuite/case-skipC","Elapsed":0}
{"Action":"run","Test":"TestSuite/case-fail"}
{"Action":"output","Test":"TestSuite/case-fail","Output":"    helper_test.go:30: boom\n"}
{"Action":"fail","Test":"TestSuite/case-fail","Elapsed":0.02}
{"Action":"pass","Test":"TestSuite","Elapsed":0.5}
{"Action":"pass","Test":"TestUnrelated","Elapsed":0.01}
`

func TestSummarize(t *testing.T) {
	s, err := Summarize(strings.NewReader(sampleJSON), Options{SuiteName: "demo", RootTest: "TestSuite"})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if s.Total != 5 {
		t.Errorf("Total = %d, want 5", s.Total)
	}
	if s.Passed != 1 {
		t.Errorf("Passed = %d, want 1", s.Passed)
	}
	if s.Failed != 1 {
		t.Errorf("Failed = %d, want 1", s.Failed)
	}
	if s.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", s.Skipped)
	}
	if got := s.SkipReasons["unsupported spec: XSLT20"]; got != 2 {
		t.Errorf("SkipReasons[XSLT20] = %d, want 2", got)
	}
	if got := s.SkipReasons["requires network"]; got != 1 {
		t.Errorf("SkipReasons[network] = %d, want 1", got)
	}
}

func TestSummarizeRequiresRootTest(t *testing.T) {
	if _, err := Summarize(strings.NewReader(""), Options{}); err == nil {
		t.Fatal("expected error when RootTest is empty")
	}
}

func TestWriteSummaryMarkdown(t *testing.T) {
	s := Summary{
		Suite: "demo", Total: 5, Passed: 1, Skipped: 3, Failed: 1,
		SkipReasons: map[string]int{"unsupported spec: XSLT20": 2, "requires network": 1},
	}
	var b strings.Builder
	if err := WriteSummaryMarkdown(&b, s, SummaryMeta{
		DisplayName: "Demo", UpstreamRepo: "https://example/repo", UpstreamCommit: "abc123",
	}); err != nil {
		t.Fatalf("WriteSummaryMarkdown: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"# Demo W3C conformance results",
		"| Pass | 1 |",
		"| Fail | 1 |",
		"| **Total** | **5** |",
		"@ `abc123`",
		"| unsupported spec: XSLT20 | 2 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, out)
		}
	}
	// Higher-count reason must sort before the lower-count one.
	if strings.Index(out, "XSLT20") > strings.Index(out, "requires network") {
		t.Errorf("skip reasons not sorted by descending count:\n%s", out)
	}
}
