package junit

import (
	"bytes"
	"strings"
	"testing"
)

func TestConvertGoTestJSONIncludesSkippedReason(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"Action":"run","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C"}`,
		`{"Action":"run","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C/ibmMeta/allGroup.testSet/s3_3_6v01"}`,
		`{"Action":"output","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C/ibmMeta/allGroup.testSet/s3_3_6v01","Output":"=== RUN   TestXSD11W3C/ibmMeta/allGroup.testSet/s3_3_6v01\n"}`,
		`{"Action":"output","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C/ibmMeta/allGroup.testSet/s3_3_6v01","Output":"    xsd11_harness_test.go:78: unsupported feature\n"}`,
		`{"Action":"skip","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C/ibmMeta/allGroup.testSet/s3_3_6v01","Elapsed":0}`,
		`{"Action":"pass","Package":"github.com/lestrrat-go/helium-w3c-tests/xsd","Test":"TestXSD11W3C","Elapsed":0}`,
		``,
	}, "\n"))

	var out bytes.Buffer
	if err := ConvertGoTestJSON(input, &out, Options{
		SuiteName: "xsd11-conformance",
		RootTest:  "TestXSD11W3C",
	}); err != nil {
		t.Fatal(err)
	}

	xml := out.String()
	if !strings.Contains(xml, `tests="1"`) {
		t.Fatalf("expected one testcase in\n%s", xml)
	}
	if !strings.Contains(xml, `skipped="1"`) {
		t.Fatalf("expected one skipped testcase in\n%s", xml)
	}
	if !strings.Contains(xml, `<skipped message="unsupported feature">`) {
		t.Fatalf("expected skipped reason in\n%s", xml)
	}
	if strings.Contains(xml, `TestXSD11W3C"`) {
		t.Fatalf("parent harness test should not be emitted as testcase in\n%s", xml)
	}
}

func TestConvertGoTestJSONIncludesSetupFailure(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"ImportPath":"github.com/example/xsd_test [github.com/example/xsd.test]","Action":"build-output","Output":"# github.com/example/xsd_test [github.com/example/xsd.test]\n"}`,
		`{"ImportPath":"github.com/example/xsd_test [github.com/example/xsd.test]","Action":"build-output","Output":"xsd/harness_test.go:1: missing API\n"}`,
		`{"ImportPath":"github.com/example/xsd_test [github.com/example/xsd.test]","Action":"build-fail"}`,
		`{"Action":"start","Package":"github.com/example/xsd"}`,
		`{"Action":"output","Package":"github.com/example/xsd","Output":"FAIL\tgithub.com/example/xsd [build failed]\n"}`,
		`{"Action":"fail","Package":"github.com/example/xsd","Elapsed":0}`,
		``,
	}, "\n"))

	var out bytes.Buffer
	if err := ConvertGoTestJSON(input, &out, Options{
		SuiteName: "xsd11-conformance",
		RootTest:  "TestXSD11W3C",
	}); err != nil {
		t.Fatal(err)
	}

	xml := out.String()
	if !strings.Contains(xml, `name="TestXSD11W3C/setup"`) {
		t.Fatalf("expected setup failure testcase in\n%s", xml)
	}
	if !strings.Contains(xml, `failures="1"`) {
		t.Fatalf("expected one failure in\n%s", xml)
	}
	if !strings.Contains(xml, `missing API`) {
		t.Fatalf("expected build output in\n%s", xml)
	}
}
