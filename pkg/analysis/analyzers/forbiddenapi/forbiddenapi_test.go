package forbiddenapi_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/forbiddenapi"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := forbiddenapi.New(forbiddenapi.Options{Policies: []forbiddenapi.Policy{
		{Package: "legacy", Symbol: "Old", Replacement: "modern.New"},
		{Package: "legacy", Symbol: "Generic", Replacement: "modern.Generic"},
		{Package: "legacy", Symbol: "Client.Call", Replacement: "ports.Client.Call"},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "consumer")
}

func TestAnalyzerAllowsReviewedPackages(t *testing.T) {
	t.Parallel()

	analyzer, err := forbiddenapi.New(forbiddenapi.Options{Policies: []forbiddenapi.Policy{{
		Package:         "legacy",
		Symbol:          "Old",
		Replacement:     "modern.New",
		AllowedPackages: []string{"approved"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}

func TestNewRejectsMalformedPolicies(t *testing.T) {
	t.Parallel()

	tests := []forbiddenapi.Policy{
		{Package: "legacy/*", Symbol: "Old", Replacement: "modern.New"},
		{Package: "legacy", Symbol: "bad-name", Replacement: "modern.New"},
		{Package: "legacy", Symbol: "Type.bad-name", Replacement: "modern.New"},
		{Package: "legacy", Symbol: "A.B.C", Replacement: "modern.New"},
		{Package: "legacy", Symbol: "Old", Replacement: " "},
		{
			Package:         "legacy",
			Symbol:          "Old",
			Replacement:     "modern.New",
			AllowedPackages: []string{"approved/*"},
		},
	}
	for _, policy := range tests {
		if _, err := forbiddenapi.New(forbiddenapi.Options{Policies: []forbiddenapi.Policy{policy}}); err == nil {
			t.Fatalf("New(%#v) error = nil", policy)
		}
	}
	duplicate := forbiddenapi.Policy{
		Package: "legacy", Symbol: "Old", Replacement: "modern.New",
	}
	if _, err := forbiddenapi.New(forbiddenapi.Options{
		Policies: []forbiddenapi.Policy{duplicate, duplicate},
	}); err == nil {
		t.Fatal("New() accepted duplicate policy")
	}
}
