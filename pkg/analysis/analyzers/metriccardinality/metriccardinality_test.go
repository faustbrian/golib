package metriccardinality_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/metriccardinality"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	variadicFrom := 1
	variadicFromStart := 0
	analyzer, err := metriccardinality.New(metriccardinality.Options{
		HighCardinalityTypes: []metriccardinality.HighCardinalityType{
			{Package: "labelmodel", Name: "RequestPath"},
			{Package: "labelmodel", Name: "UserID"},
		},
		Sinks: []metriccardinality.Sink{
			{Package: "metricsink", Symbol: "Label", Arguments: []int{0}},
			{Package: "metricsink", Symbol: "Positioned", Arguments: []int{0}},
			{Package: "metricsink", Symbol: "Variadic", VariadicFrom: &variadicFrom},
			{Package: "metricsink", Symbol: "All", VariadicFrom: &variadicFromStart},
			{Package: "metricsink", Symbol: "Meter.Record", Arguments: []int{1}},
			{Package: "metricsink", Symbol: "Meter.PointerRecord", Arguments: []int{1}},
			{Package: "metricsink", Symbol: "Generic", Arguments: []int{0}},
			{Package: "metricsink", Symbol: "GenericPair", Arguments: []int{1}},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "metricconsumer")
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		metriccardinality.Analyzer,
		"metricunconfigured",
	)
}

func TestLabelNameAnalyzer(t *testing.T) {
	t.Parallel()

	variadicFrom := 1
	analyzer, err := metriccardinality.NewLabelName(
		metriccardinality.LabelNameOptions{
			AttackerControlledTypes: []metriccardinality.AttackerControlledType{{
				Package: "labelmodel", Name: "LabelName",
			}},
			Sinks: []metriccardinality.Sink{
				{Package: "metricsink", Symbol: "Label", Arguments: []int{0}},
				{Package: "metricsink", Symbol: "Generic", Arguments: []int{0}},
				{Package: "metricsink", Symbol: "Variadic", VariadicFrom: &variadicFrom},
				{Package: "metricsink", Symbol: "Meter.Record", Arguments: []int{0}},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewLabelName() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "metricnameconsumer")
}

func TestUnconfiguredLabelNameAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		metriccardinality.LabelNameAnalyzer,
		"metricunconfigured",
	)
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	zero := 0
	negative := -1
	validType := []metriccardinality.HighCardinalityType{{
		Package: "labelmodel", Name: "UserID",
	}}
	validSink := []metriccardinality.Sink{{
		Package: "metricsink", Symbol: "Label", Arguments: []int{0},
	}}
	tests := []metriccardinality.Options{
		{HighCardinalityTypes: []metriccardinality.HighCardinalityType{{Package: "labelmodel/*", Name: "UserID"}}, Sinks: validSink},
		{HighCardinalityTypes: []metriccardinality.HighCardinalityType{{Package: "labelmodel", Name: "bad-name"}}, Sinks: validSink},
		{HighCardinalityTypes: append(validType, validType...), Sinks: validSink},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink/*", Symbol: "Label", Arguments: []int{0}}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "bad-name", Arguments: []int{0}}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "A.B.C", Arguments: []int{0}}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "Label"}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "Label", Arguments: []int{-1}}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "Label", Arguments: []int{0, 0}}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "Label", VariadicFrom: &negative}}},
		{HighCardinalityTypes: validType, Sinks: []metriccardinality.Sink{{Package: "metricsink", Symbol: "Label", Arguments: []int{0}}, {Package: "metricsink", Symbol: "Label", VariadicFrom: &zero}}},
	}
	for _, options := range tests {
		if _, err := metriccardinality.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}

func TestNewAcceptsZeroVariadicPosition(t *testing.T) {
	t.Parallel()

	zero := 0
	_, err := metriccardinality.New(metriccardinality.Options{
		HighCardinalityTypes: []metriccardinality.HighCardinalityType{{
			Package: "labelmodel",
			Name:    "UserID",
		}},
		Sinks: []metriccardinality.Sink{{
			Package:      "metricsink",
			Symbol:       "Variadic",
			VariadicFrom: &zero,
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
}
