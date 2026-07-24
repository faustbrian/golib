package sensitivesink_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/sensitivesink"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	variadicFrom := 1
	analyzer, err := sensitivesink.New(sensitivesink.Options{
		SensitiveTypes: []sensitivesink.SensitiveType{
			{Package: "secretmodel", Name: "Credentials"},
			{Package: "secretmodel", Name: "Token"},
		},
		Sinks: []sensitivesink.Sink{
			{Package: "sinkapi", Symbol: "Record", Arguments: []int{0}},
			{Package: "sinkapi", Symbol: "RecordThree", Arguments: []int{2}},
			{Package: "sinkapi", Symbol: "Format", VariadicFrom: &variadicFrom},
			{Package: "sinkapi", Symbol: "All", VariadicFrom: new(int)},
			{Package: "sinkapi", Symbol: "Logger.Log", Arguments: []int{1}},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "consumer")
}

func TestAnalyzerIgnoresUnconfiguredPolicy(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		sensitivesink.Analyzer,
		"approved",
	)
}

func TestAnalyzerAllowsReviewedSinkPackages(t *testing.T) {
	t.Parallel()

	analyzer, err := sensitivesink.New(sensitivesink.Options{
		SensitiveTypes: []sensitivesink.SensitiveType{{
			Package: "secretmodel", Name: "Token",
		}},
		Sinks: []sensitivesink.Sink{{
			Package:         "sinkapi",
			Symbol:          "Record",
			Arguments:       []int{0},
			AllowedPackages: []string{"approved"},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	zero := 0
	negative := -1
	validType := []sensitivesink.SensitiveType{{Package: "secretmodel", Name: "Token"}}
	validSink := []sensitivesink.Sink{{
		Package: "sinkapi", Symbol: "Record", Arguments: []int{0},
	}}
	tests := []sensitivesink.Options{
		{SensitiveTypes: []sensitivesink.SensitiveType{{Package: "secretmodel/*", Name: "Token"}}, Sinks: validSink},
		{SensitiveTypes: []sensitivesink.SensitiveType{{Package: "secretmodel", Name: "bad-name"}}, Sinks: validSink},
		{SensitiveTypes: append(validType, validType...), Sinks: validSink},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi/*", Symbol: "Record", Arguments: []int{0}}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "bad-name", Arguments: []int{0}}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "A.B.C", Arguments: []int{0}}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record"}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record", Arguments: []int{-1}}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record", Arguments: []int{0, 0}}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record", VariadicFrom: &negative}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record", Arguments: []int{0}}, {Package: "sinkapi", Symbol: "Record", VariadicFrom: &zero}}},
		{SensitiveTypes: validType, Sinks: []sensitivesink.Sink{{Package: "sinkapi", Symbol: "Record", Arguments: []int{0}, AllowedPackages: []string{"approved/*"}}}},
	}
	for _, options := range tests {
		if _, err := sensitivesink.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}
