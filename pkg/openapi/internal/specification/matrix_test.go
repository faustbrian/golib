package specification

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestExtractNormativeOccurrencesPreservesSectionAndCompoundKeywords(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`# OpenAPI Specification

The words MUST, MUST NOT, and MAY are interpreted as described in BCP 14.

## Paths Object

Each template expression MUST NOT be ambiguous and tooling MAY reject it.
Lowercase wording should not be treated as normative even when it says must.
`)

	occurrences, err := ExtractNormative("3.2.0", "oas/3.2/3.2.0.md", input)
	if err != nil {
		t.Fatalf("ExtractNormative() error = %v", err)
	}

	if got, want := len(occurrences), 2; got != want {
		t.Fatalf("len(occurrences) = %d, want %d", got, want)
	}

	if got, want := occurrences[0].ID, "OAS-3.2.0-0001"; got != want {
		t.Errorf("first ID = %q, want %q", got, want)
	}
	if got, want := occurrences[0].Keyword, "MUST NOT"; got != want {
		t.Errorf("first keyword = %q, want %q", got, want)
	}
	if got, want := occurrences[1].Keyword, "MAY"; got != want {
		t.Errorf("second keyword = %q, want %q", got, want)
	}
	if got, want := occurrences[0].Section, "Paths Object"; got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
	if got, want := occurrences[0].Line, 7; got != want {
		t.Errorf("line = %d, want %d", got, want)
	}
	if strings.Contains(occurrences[0].Text, "\t") {
		t.Error("requirement text contains a tab")
	}
}

func TestNormativeInventoryValidatesInputsAndPropagatesIO(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		version string
		source  string
		reader  io.Reader
	}{
		{source: "spec.md", reader: strings.NewReader("MUST")},
		{version: "3.2.0", reader: strings.NewReader("MUST")},
		{version: "3.2.0", source: "spec.md"},
		{version: "3.2.0", source: "spec.md",
			reader: strings.NewReader(strings.Repeat("x", maxSpecificationLineBytes+1))},
	} {
		if _, err := ExtractNormative(test.version, test.source, test.reader); err == nil {
			t.Fatalf("ExtractNormative(%q, %q) error = nil", test.version, test.source)
		}
	}
	if err := WriteNormativeTSV(nil, nil); err == nil {
		t.Fatal("WriteNormativeTSV accepted a nil writer")
	}
	if err := WriteInitialEvidenceTSV(nil, nil); err == nil {
		t.Fatal("WriteInitialEvidenceTSV accepted a nil writer")
	}
	failure := errors.New("write failed")
	if err := WriteNormativeTSV(errorWriter{failure}, []Occurrence{{ID: "x"}}); !errors.Is(err, failure) {
		t.Fatalf("WriteNormativeTSV error = %v", err)
	}
	if err := WriteInitialEvidenceTSV(errorWriter{failure}, []Occurrence{{ID: "x"}}); !errors.Is(err, failure) {
		t.Fatalf("WriteInitialEvidenceTSV error = %v", err)
	}
}

type errorWriter struct{ err error }

func (writer errorWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestExtractNormativeRejectsMissingVersion(t *testing.T) {
	t.Parallel()

	_, err := ExtractNormative("", "spec.md", strings.NewReader("A value MUST exist."))
	if err == nil {
		t.Fatal("ExtractNormative() error = nil, want error")
	}
}

func TestWriteNormativeTSVIsDeterministic(t *testing.T) {
	t.Parallel()

	occurrences := []Occurrence{{
		ID:      "OAS-3.2.0-0001",
		Version: "3.2.0",
		Source:  "oas/3.2/3.2.0.md",
		Line:    42,
		Section: "Paths Object",
		Keyword: "MUST",
		Text:    "A path MUST begin with a slash.",
	}}

	var output bytes.Buffer
	if err := WriteNormativeTSV(&output, occurrences); err != nil {
		t.Fatalf("WriteNormativeTSV() error = %v", err)
	}

	want := "id\tversion\tsource\tline\tsection\tkeyword\ttext\n" +
		"OAS-3.2.0-0001\t3.2.0\toas/3.2/3.2.0.md\t42\tPaths Object\tMUST\tA path MUST begin with a slash.\n"
	if got := output.String(); got != want {
		t.Fatalf("WriteNormativeTSV() = %q, want %q", got, want)
	}
}

func TestWriteInitialEvidenceTSVMarksEveryOccurrenceUnimplemented(t *testing.T) {
	t.Parallel()

	occurrences := []Occurrence{{ID: "OAS-2.0-0001"}, {ID: "OAS-3.2.0-0001"}}

	var output bytes.Buffer
	if err := WriteInitialEvidenceTSV(&output, occurrences); err != nil {
		t.Fatalf("WriteInitialEvidenceTSV() error = %v", err)
	}

	want := "id\tstatus\timplementation\ttests\tdocumentation\tnotes\n" +
		"OAS-2.0-0001\tunimplemented\t\t\t\t\n" +
		"OAS-3.2.0-0001\tunimplemented\t\t\t\t\n"
	if got := output.String(); got != want {
		t.Fatalf("WriteInitialEvidenceTSV() = %q, want %q", got, want)
	}
}
