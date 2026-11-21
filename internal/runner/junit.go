package runner

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

type junitTestSuites struct {
	Suites []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string           `xml:"name,attr"`
	File      string           `xml:"file,attr"`
	Suites    []junitTestSuite `xml:"testsuite"`
	TestCases []junitTestCase  `xml:"testcase"`
}

type junitTestCase struct {
	Name      string         `xml:"name,attr"`
	Classname string         `xml:"classname,attr"`
	File      string         `xml:"file,attr"`
	Failures  []junitMessage `xml:"failure"`
	Errors    []junitMessage `xml:"error"`
	Skipped   []junitMessage `xml:"skipped"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// ParseJUnit parses a JUnit XML report (`<testsuites>` or a single `<testsuite>`).
// Test counts are computed from `<testcase>` elements rather than suite attributes.
func ParseJUnit(data []byte) (Result, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Result{}, fmt.Errorf("parse junit xml: empty input")
	}

	dec := xml.NewDecoder(bytes.NewReader(trimmed))
	var root xml.StartElement
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Result{}, fmt.Errorf("parse junit xml: no root element")
			}
			return Result{}, fmt.Errorf("parse junit xml: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			root = se
			break
		}
	}

	var suites []junitTestSuite
	switch root.Name.Local {
	case "testsuites":
		var doc junitTestSuites
		if err := dec.DecodeElement(&doc, &root); err != nil {
			return Result{}, fmt.Errorf("parse junit xml: %w", err)
		}
		suites = doc.Suites
	case "testsuite":
		var suite junitTestSuite
		if err := dec.DecodeElement(&suite, &root); err != nil {
			return Result{}, fmt.Errorf("parse junit xml: %w", err)
		}
		suites = []junitTestSuite{suite}
	default:
		return Result{}, fmt.Errorf("parse junit xml: unexpected root element <%s>", root.Name.Local)
	}

	res := Result{Format: FormatJUnit}
	for i := range suites {
		collectJUnitSuite(&res, &suites[i], "")
	}
	return res, nil
}

func collectJUnitSuite(res *Result, suite *junitTestSuite, inheritedFile string) {
	suiteFile := suite.File
	if suiteFile == "" {
		suiteFile = inheritedFile
	}

	for _, tc := range suite.TestCases {
		res.Total++
		switch {
		case len(tc.Failures) > 0 || len(tc.Errors) > 0:
			res.Failed++
			var msg junitMessage
			if len(tc.Failures) > 0 {
				msg = tc.Failures[0]
			} else {
				msg = tc.Errors[0]
			}
			file := tc.File
			if file == "" {
				file = suiteFile
			}
			res.Failures = append(res.Failures, TestCase{
				Name:    junitCaseName(tc),
				File:    file,
				Status:  "failed",
				Message: formatJUnitMessage(msg),
			})
		case len(tc.Skipped) > 0:
			res.Skipped++
		default:
			res.Passed++
		}
	}

	for i := range suite.Suites {
		collectJUnitSuite(res, &suite.Suites[i], suiteFile)
	}
}

func junitCaseName(tc junitTestCase) string {
	cls := strings.TrimSpace(tc.Classname)
	name := strings.TrimSpace(tc.Name)
	if cls == "" {
		return name
	}
	if name == "" {
		return cls
	}
	if strings.Contains(name, cls) {
		return name
	}
	return cls + "." + name
}

func formatJUnitMessage(m junitMessage) string {
	attr := cleanMessage(m.Message)
	body := cleanMessage(m.Body)

	if attr == "" {
		return body
	}
	if body == "" {
		return attr
	}
	// Runners like go-junit-report put a generic "Failed" in message="" and put
	// the actual failure output in the element body.
	if strings.EqualFold(attr, "failed") || strings.EqualFold(attr, "error") {
		return body
	}
	return attr
}
