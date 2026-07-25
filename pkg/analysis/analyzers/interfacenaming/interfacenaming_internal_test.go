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
