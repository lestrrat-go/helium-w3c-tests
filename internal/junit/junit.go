package junit

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type Options struct {
	SuiteName string
	RootTest  string
}

type event struct {
	Action     string  `json:"Action"`
	ImportPath string  `json:"ImportPath"`
	Package    string  `json:"Package"`
	Test       string  `json:"Test"`
	Output     string  `json:"Output"`
	Elapsed    float64 `json:"Elapsed"`
}

type result struct {
	Package string
	Name    string
	Status  string
	Elapsed float64
	Output  []string
}

type testsuites struct {
	XMLName  xml.Name    `xml:"testsuites"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr"`
	Suites   []testsuite `xml:"testsuite"`
}

type testsuite struct {
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Failures  int        `xml:"failures,attr"`
	Skipped   int        `xml:"skipped,attr"`
	Time      string     `xml:"time,attr"`
	TestCases []testcase `xml:"testcase"`
}

type testcase struct {
	ClassName string      `xml:"classname,attr"`
	Name      string      `xml:"name,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *failure    `xml:"failure,omitempty"`
	Skipped   *skipped    `xml:"skipped,omitempty"`
	SystemOut *systemText `xml:"system-out,omitempty"`
}

type failure struct {
	Message string `xml:"message,attr,omitempty"`
	Output  string `xml:",chardata"`
}

type skipped struct {
	Message string `xml:"message,attr,omitempty"`
	Output  string `xml:",chardata"`
}

type systemText struct {
	Output string `xml:",chardata"`
}

var testLogPrefix = regexp.MustCompile(`^\s*[^:\s]+\.go:\d+:\s*`)

func ConvertGoTestJSON(r io.Reader, w io.Writer, opt Options) error {
	if opt.SuiteName == "" {
		opt.SuiteName = "go-test"
	}
	if opt.RootTest == "" {
		return fmt.Errorf("root test is required")
	}

	records := make(map[string]*result)
	order := make([]string, 0)
	var packageName string
	var packageFailed bool
	var packageOutput []string
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if ev.Package != "" {
			packageName = ev.Package
		} else if ev.ImportPath != "" {
			packageName = ev.ImportPath
		}
		if ev.Test == "" {
			switch ev.Action {
			case "build-output", "output":
				packageOutput = append(packageOutput, ev.Output)
			case "build-fail", "fail":
				packageFailed = true
			}
		}
		if !strings.HasPrefix(ev.Test, opt.RootTest+"/") {
			continue
		}
		rec := records[ev.Test]
		if rec == nil {
			rec = &result{Name: ev.Test, Package: ev.Package}
			records[ev.Test] = rec
			order = append(order, ev.Test)
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			rec.Status = ev.Action
			rec.Elapsed = ev.Elapsed
		case "output":
			rec.Output = append(rec.Output, ev.Output)
		}
	}
	if len(order) == 0 && packageFailed {
		name := opt.RootTest + "/setup"
		records[name] = &result{
			Package: packageName,
			Name:    name,
			Status:  "fail",
			Output:  packageOutput,
		}
		order = append(order, name)
	}
	sort.Strings(order)

	suite := testsuite{Name: opt.SuiteName}
	var totalTime float64
	var suiteTime float64
	for _, name := range order {
		rec := records[name]
		if rec.Status == "" {
			continue
		}
		tc := testcase{
			ClassName: rec.Package,
			Name:      rec.Name,
			Time:      formatSeconds(rec.Elapsed),
		}
		output := strings.Join(rec.Output, "")
		switch rec.Status {
		case "fail":
			suite.Failures++
			tc.Failure = &failure{Message: firstMeaningfulLine(output), Output: output}
		case "skip":
			suite.Skipped++
			msg := firstMeaningfulLine(output)
			tc.Skipped = &skipped{Message: msg, Output: output}
		}
		if strings.TrimSpace(output) != "" {
			tc.SystemOut = &systemText{Output: output}
		}
		suite.Tests++
		suiteTime += rec.Elapsed
		totalTime += rec.Elapsed
		suite.TestCases = append(suite.TestCases, tc)
	}
	suite.Time = formatSeconds(suiteTime)

	doc := testsuites{
		Tests:    suite.Tests,
		Failures: suite.Failures,
		Skipped:  suite.Skipped,
		Time:     formatSeconds(totalTime),
		Suites:   []testsuite{suite},
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Flush()
}

func firstMeaningfulLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "===") || strings.HasPrefix(line, "---") {
			continue
		}
		return testLogPrefix.ReplaceAllString(line, "")
	}
	return ""
}

func formatSeconds(v float64) string {
	return fmt.Sprintf("%.3f", v)
}
