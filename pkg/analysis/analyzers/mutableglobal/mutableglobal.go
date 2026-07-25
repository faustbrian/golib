// Package mutableglobal reports package variables that hold shared mutable
// state rather than immutable scalar values.
package mutableglobal

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "safety/no-mutable-global"

// Policy selects one package tree for shared-state enforcement.
type Policy struct {
	Package string
}

// Options configures package trees governed by shared-state policy.
type Options struct {
	Policies []Policy
}

type packagePattern struct {
	prefix string
	tree   bool
}

type compiledPolicy struct {
	packagePattern packagePattern
}

// Rule is the stable metadata for shared package-state policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategorySafety,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Composite package variables hide shared ownership and mutable process state.",
	Remediation:       "Construct the state explicitly and inject its owner into dependent components.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"mutable_globals": {
			Type:        shared.ConfigurationArray,
			Description: "Package trees governed by shared mutable state policy.",
		},
	}},
}

// Analyzer is inactive until package trees are explicitly configured.
var Analyzer, _ = New(Options{})

// New validates policies and constructs an independently configured analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	policies := make([]compiledPolicy, 0, len(options.Policies))
	for _, policy := range options.Policies {
		pattern, err := compilePattern(policy.Package)
		if err != nil {
			return nil, fmt.Errorf("mutable global package pattern: %w", err)
		}
		for _, existing := range policies {
			if pattern.overlaps(existing.packagePattern) {
				return nil, fmt.Errorf("mutable global package %q overlaps another policy", policy.Package)
			}
		}
		policies = append(policies, compiledPolicy{packagePattern: pattern})
	}

	return &analysis.Analyzer{
		Name: "mutableglobal",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			return run(pass, policies)
		},
	}, nil
}

func run(pass *analysis.Pass, policies []compiledPolicy) (any, error) {
	if !governs(policies, pass.Pkg.Path()) {
		return nil, nil
	}
	errorType := types.Universe.Lookup("error").Type()
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value := specification.(*ast.ValueSpec)
				for _, name := range value.Names {
					object := pass.TypesInfo.Defs[name]
					if name.Name == "_" || object == nil ||
						!holdsMutableState(object.Type(), errorType) {
						continue
					}
					pass.Reportf(
						name.Pos(),
						"%s: package variable %s holds shared mutable state",
						ruleID,
						name.Name,
					)
				}
			}
		}
	}

	return nil, nil
}

func governs(policies []compiledPolicy, packagePath string) bool {
	for _, policy := range policies {
		if policy.packagePattern.matches(packagePath) {
			return true
		}
	}

	return false
}

func compilePattern(value string) (packagePattern, error) {
	if value == "" || strings.Contains(value, "*") {
		return packagePattern{}, errors.New("pattern must be exact or use a package-tree suffix")
	}
	tree := strings.HasSuffix(value, "/...")
	prefix := strings.TrimSuffix(value, "/...")
	if strings.Contains(prefix, "...") || path.Clean(prefix) != prefix ||
		strings.HasPrefix(prefix, "/") || prefix == "." {
		return packagePattern{}, errors.New("pattern must be a clean import path")
	}

	return packagePattern{prefix: prefix, tree: tree}, nil
}

func (pattern packagePattern) matches(value string) bool {
	return value == pattern.prefix ||
		(pattern.tree && strings.HasPrefix(value, pattern.prefix+"/"))
}

func (pattern packagePattern) overlaps(other packagePattern) bool {
	return pattern.matches(other.prefix) || other.matches(pattern.prefix)
}

func holdsMutableState(candidate types.Type, errorType types.Type) bool {
	if types.Identical(candidate, errorType) {
		return false
	}
	candidate = types.Unalias(candidate)
	switch typed := candidate.(type) {
	case *types.Basic:
		return typed.Kind() == types.UnsafePointer
	case *types.Named:
		return holdsMutableState(typed.Underlying(), errorType)
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan,
		*types.Signature, *types.Interface, *types.Struct, *types.Array:
		return true
	default:
		return false
	}
}
