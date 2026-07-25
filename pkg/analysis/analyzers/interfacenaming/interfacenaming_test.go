package interfacenaming_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/interfacenaming"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := interfacenaming.New(interfacenaming.Options{Policies: []interfacenaming.Policy{{
		Package:        "interfaces/provider/...",
		RequiredPrefix: "Order",
		RequiredSuffix: "Port",
		AllowedNames:   []string{"Compatibility"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(
		t,
		analysistest.TestData(),
		analyzer,
		"interfaces/provider",
		"interfaces/provider/child",
		"interfaces/other",
	)
}

func TestAnalyzerIgnoresUnconfiguredPolicy(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		interfacenaming.Analyzer,
		"interfaces/other",
	)
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	valid := interfacenaming.Policy{Package: "interfaces/provider", RequiredSuffix: "Port"}
	tests := []interfacenaming.Options{
		{Policies: []interfacenaming.Policy{{Package: "interfaces/*", RequiredSuffix: "Port"}}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider"}}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider", RequiredPrefix: "order"}}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider", RequiredSuffix: "bad-name"}}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider", RequiredSuffix: "Port", AllowedNames: []string{"compatibility"}}}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider", RequiredSuffix: "Port", AllowedNames: []string{"Compatibility", "Compatibility"}}}},
		{Policies: []interfacenaming.Policy{valid, valid}},
		{Policies: []interfacenaming.Policy{{Package: "interfaces/provider/...", RequiredSuffix: "Port"}, {Package: "interfaces/provider/child", RequiredSuffix: "Port"}}},
	}
	for _, options := range tests {
		if _, err := interfacenaming.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}
