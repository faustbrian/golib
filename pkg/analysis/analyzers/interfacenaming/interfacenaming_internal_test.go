package interfacenaming

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestNewAcceptsAndRejectsPolicyCountBoundaries(t *testing.T) {
	t.Parallel()

	policies := make([]Policy, maxPolicies)
	for index := range policies {
		policies[index] = Policy{
			Package:        fmt.Sprintf("example.com/p%d", index),
			RequiredSuffix: "Port",
		}
	}
	if _, err := New(Options{Policies: policies}); err != nil {
		t.Fatalf("New(exact policy limit) error = %v", err)
	}
	policies = append(policies, Policy{Package: "example.com/overflow", RequiredSuffix: "Port"})
	if _, err := New(Options{Policies: policies}); err == nil {
		t.Fatal("New() accepted policy count above limit")
	}
}

func TestReportDeclarationIgnoresMissingTypeInformation(t *testing.T) {
	t.Parallel()

	name := ast.NewIdent("Client")
	declaration := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{
		&ast.TypeSpec{Name: name, Type: ast.NewIdent("any")},
	}}
	reported := false
	reportDeclaration(&analysis.Pass{
		TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}},
		Report:    func(analysis.Diagnostic) { reported = true },
	}, declaration, compiledPolicy{suffix: "Port"})
	if reported {
		t.Fatal("reportDeclaration() reported without type information")
	}
}

func TestReportDeclarationContinuesAfterMissingTypeInformation(t *testing.T) {
	t.Parallel()

	missing := ast.NewIdent("Missing")
	client := ast.NewIdent("Client")
	declaration := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{
		&ast.TypeSpec{Name: missing, Type: &ast.InterfaceType{Methods: &ast.FieldList{}}},
		&ast.TypeSpec{Name: client, Type: &ast.InterfaceType{Methods: &ast.FieldList{}}},
	}}
	interfaceType := types.NewInterfaceType(nil, nil).Complete()
	reported := 0
	reportDeclaration(&analysis.Pass{
		TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{
			client: types.NewTypeName(token.NoPos, nil, client.Name, interfaceType),
		}},
		Report: func(analysis.Diagnostic) { reported++ },
	}, declaration, compiledPolicy{suffix: "Port"})
	if reported != 1 {
		t.Fatalf("reportDeclaration() reports = %d, want 1", reported)
	}
}

func TestPackagePatternValidationDimensions(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"", ".", "interfaces/*", `interfaces\provider`, "interfaces/.../provider"} {
		if _, _, valid := packagePattern(pattern); valid {
			t.Errorf("packagePattern(%q) valid = true", pattern)
		}
	}
	base, tree, valid := packagePattern("interfaces/provider/...")
	if base != "interfaces/provider" || !tree || !valid {
		t.Fatalf("packagePattern(tree) = (%q, %t, %t)", base, tree, valid)
	}
}

func TestPoliciesOverlapOnlyForExactOrTreeDescendantPaths(t *testing.T) {
	t.Parallel()

	policy := func(packagePath string, tree bool) compiledPolicy {
		return compiledPolicy{packagePath: packagePath, tree: tree}
	}
	tests := []struct {
		name  string
		left  compiledPolicy
		right compiledPolicy
		want  bool
	}{
		{name: "exact", left: policy("example.com/ports", false), right: policy("example.com/ports", false), want: true},
		{name: "left tree", left: policy("example.com/ports", true), right: policy("example.com/ports/http", false), want: true},
		{name: "right tree", left: policy("example.com/ports/http", false), right: policy("example.com/ports", true), want: true},
		{name: "left exact parent", left: policy("example.com/ports", false), right: policy("example.com/ports/http", false), want: false},
		{name: "right exact parent", left: policy("example.com/ports/http", false), right: policy("example.com/ports", false), want: false},
		{name: "path prefix only", left: policy("example.com/port", true), right: policy("example.com/ports/http", false), want: false},
		{name: "disjoint", left: policy("example.com/ports", true), right: policy("example.com/adapters", true), want: false},
	}
	for _, test := range tests {
		if got := policiesOverlap(test.left, test.right); got != test.want {
			t.Errorf("%s: policiesOverlap() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestCompilePolicyAcceptsAndRejectsAllowedNameBoundaries(t *testing.T) {
	t.Parallel()

	names := make([]string, maxAllowedNamesEach)
	for index := range names {
		names[index] = fmt.Sprintf("Compatibility%d", index)
	}
	policy := Policy{
		Package:        "example.com/ports",
		RequiredSuffix: "Port",
		AllowedNames:   names,
	}
	if _, err := compilePolicy(policy); err != nil {
		t.Fatalf("compilePolicy(exact name limit) error = %v", err)
	}
	policy.AllowedNames = append(policy.AllowedNames, "Overflow")
	if _, err := compilePolicy(policy); err == nil {
		t.Fatal("compilePolicy() accepted compatibility names above limit")
	}
}

func TestNameRequirements(t *testing.T) {
	t.Parallel()

	if got := requirement(compiledPolicy{prefix: "Order"}); got != "must start with Order" {
		t.Fatalf("prefix requirement = %q", got)
	}
	if got := requirement(compiledPolicy{suffix: "Port"}); got != "must end with Port" {
		t.Fatalf("suffix requirement = %q", got)
	}
	both := compiledPolicy{prefix: "Order", suffix: "Port", allowed: map[string]struct{}{
		"Compatibility": {},
	}}
	for name, want := range map[string]bool{
		"Compatibility":   true,
		"OrderClientPort": true,
		"OrderClient":     false,
		"ClientPort":      false,
	} {
		if got := nameAllowed(name, both); got != want {
			t.Errorf("nameAllowed(%q) = %t, want %t", name, got, want)
		}
	}
}
