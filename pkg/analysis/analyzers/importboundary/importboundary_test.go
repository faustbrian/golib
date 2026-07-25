package importboundary_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/importboundary"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := importboundary.New(importboundary.Options{
		RestrictedDependencies: []importboundary.RestrictedDependency{{
			Package:         "arch/backend/...",
			AllowedPackages: []string{"arch/adapter/..."},
		}},
		Policies: []importboundary.Policy{{
			Package:      "arch/domain/...",
			DenyImports:  []string{"arch/infra/..."},
			AllowImports: []string{"arch/infra/approved"},
		}},
		Packages: []importboundary.PackageClass{
			{Package: "arch/aliasuse/...", Layer: "application", Context: "orders"},
			{Package: "arch/application/...", Layer: "application", Context: "orders"},
			{Package: "arch/catalog/...", Layer: "domain", Context: "catalog"},
			{Package: "arch/contextonly/...", Context: "orders"},
			{Package: "arch/infra/...", Layer: "infrastructure", Context: "orders"},
			{Package: "arch/layeronly/...", Layer: "application"},
			{Package: "arch/shared/...", Layer: "shared", Context: "shared"},
		},
		Layers: []importboundary.Direction{
			{Name: "application", MayImport: []string{"domain", "shared"}},
			{Name: "domain", MayImport: []string{"shared"}},
			{Name: "infrastructure", MayImport: []string{"application", "domain", "shared"}},
			{Name: "shared"},
		},
		Contexts: []importboundary.Direction{
			{Name: "orders", MayImport: []string{"shared"}},
			{Name: "catalog", MayImport: []string{"shared"}},
			{Name: "shared"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(
		t,
		analysistest.TestData(),
		analyzer,
		"arch/aliasuse",
		"arch/adapter",
		"arch/application",
		"arch/backendconsumer",
		"arch/contextonly",
		"arch/domain",
		"arch/layeronly",
		"arch/shared",
		"arch/unclassified",
	)
}

func TestNewRejectsAmbiguousPatterns(t *testing.T) {
	t.Parallel()

	tests := map[string]importboundary.Options{
		"empty package": {Policies: []importboundary.Policy{
			{Package: "", DenyImports: []string{"arch/infra"}},
		}},
		"middle ellipsis": {Policies: []importboundary.Policy{
			{Package: "arch/.../domain", DenyImports: []string{"arch/infra"}},
		}},
		"glob package": {Policies: []importboundary.Policy{
			{Package: "arch/domain/*", DenyImports: []string{"arch/infra"}},
		}},
		"empty deny list": {Policies: []importboundary.Policy{
			{Package: "arch/domain"},
		}},
		"invalid deny": {Policies: []importboundary.Policy{
			{Package: "arch/domain", DenyImports: []string{"arch/*"}},
		}},
		"invalid allow": {Policies: []importboundary.Policy{{
			Package: "arch/domain", DenyImports: []string{"arch/infra"}, AllowImports: []string{"arch/*"},
		}}},
		"overlapping boundaries": {Policies: []importboundary.Policy{
			{Package: "arch/domain/...", DenyImports: []string{"arch/infra"}},
			{Package: "arch/domain/model", DenyImports: []string{"arch/legacy"}},
		}},
		"invalid layer name": {Layers: []importboundary.Direction{{Name: "Application"}}},
		"empty layer name":   {Layers: []importboundary.Direction{{Name: ""}}},
		"numeric layer name": {Layers: []importboundary.Direction{{Name: "1-domain"}}},
		"unicode layer name": {Layers: []importboundary.Direction{{Name: "dömain"}}},
		"double hyphen name": {Layers: []importboundary.Direction{{Name: "domain--model"}}},
		"duplicate layer":    {Layers: []importboundary.Direction{{Name: "domain"}, {Name: "domain"}}},
		"unknown layer target": {Layers: []importboundary.Direction{
			{Name: "domain", MayImport: []string{"missing"}},
		}},
		"self layer target": {Layers: []importboundary.Direction{
			{Name: "domain", MayImport: []string{"domain"}},
		}},
		"duplicate layer target": {Layers: []importboundary.Direction{
			{Name: "domain", MayImport: []string{"shared", "shared"}},
			{Name: "shared"},
		}},
		"direct layer cycle": {Layers: []importboundary.Direction{
			{Name: "application", MayImport: []string{"domain"}},
			{Name: "domain", MayImport: []string{"application"}},
		}},
		"transitive context cycle": {Contexts: []importboundary.Direction{
			{Name: "catalog", MayImport: []string{"shared"}},
			{Name: "orders", MayImport: []string{"catalog"}},
			{Name: "shared", MayImport: []string{"orders"}},
		}},
		"invalid context name": {Contexts: []importboundary.Direction{{Name: "orders_legacy"}}},
		"invalid classified pattern": {Packages: []importboundary.PackageClass{
			{Package: "arch/*", Layer: "domain"},
		}},
		"empty classification": {Packages: []importboundary.PackageClass{
			{Package: "arch/domain"},
		}},
		"unknown classified layer": {Packages: []importboundary.PackageClass{
			{Package: "arch/domain", Layer: "domain"},
		}},
		"unknown classified context": {Packages: []importboundary.PackageClass{
			{Package: "arch/domain", Context: "orders"},
		}},
		"overlapping classifications": {
			Layers: []importboundary.Direction{{Name: "domain"}},
			Packages: []importboundary.PackageClass{
				{Package: "arch/domain/...", Layer: "domain"},
				{Package: "arch/domain/model", Layer: "domain"},
			},
		},
		"empty restricted dependency": {RestrictedDependencies: []importboundary.RestrictedDependency{
			{Package: "", AllowedPackages: []string{"arch/adapter"}},
		}},
		"restricted dependency without adapters": {
			RestrictedDependencies: []importboundary.RestrictedDependency{{Package: "arch/backend"}},
		},
		"invalid adapter pattern": {RestrictedDependencies: []importboundary.RestrictedDependency{{
			Package: "arch/backend", AllowedPackages: []string{"arch/*"},
		}}},
		"duplicate adapter pattern": {RestrictedDependencies: []importboundary.RestrictedDependency{{
			Package: "arch/backend", AllowedPackages: []string{"arch/adapter", "arch/adapter"},
		}}},
		"overlapping adapter patterns": {RestrictedDependencies: []importboundary.RestrictedDependency{{
			Package: "arch/backend", AllowedPackages: []string{"arch/adapter/...", "arch/adapter/sql"},
		}}},
		"overlapping restricted dependencies": {
			RestrictedDependencies: []importboundary.RestrictedDependency{
				{Package: "arch/backend/...", AllowedPackages: []string{"arch/adapter"}},
				{Package: "arch/backend/client", AllowedPackages: []string{"arch/adapter"}},
			},
		},
	}
	for name, options := range tests {
		_, err := importboundary.New(options)
		if err == nil {
			t.Fatalf("New() accepted invalid %s configuration", name)
		}
	}
}

func TestNewReportsDeterministicDirectionCycle(t *testing.T) {
	t.Parallel()

	_, err := importboundary.New(importboundary.Options{
		Layers: []importboundary.Direction{
			{Name: "shared", MayImport: []string{"application"}},
			{Name: "domain", MayImport: []string{"shared"}},
			{Name: "application", MayImport: []string{"domain"}},
		},
	})
	if err == nil {
		t.Fatal("New() accepted a cyclic layer graph")
	}
	const want = "layer dependency cycle: application -> domain -> shared -> application"
	if err.Error() != want {
		t.Fatalf("New() error = %q, want %q", err, want)
	}
}

func TestNewAcceptsBoundaryArchitectureName(t *testing.T) {
	t.Parallel()

	if _, err := importboundary.New(importboundary.Options{
		Layers: []importboundary.Direction{{Name: "z09"}},
	}); err != nil {
		t.Fatalf("New() error = %v", err)
	}
}
