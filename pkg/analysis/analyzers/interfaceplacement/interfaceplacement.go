// Package interfaceplacement enforces consumer-owned public interfaces in
// configured provider package trees.
package interfaceplacement

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

const ruleID = "api/interface-placement"

// Options configures provider package trees that must not own public value
// interfaces.
type Options struct {
	Packages []string
}

type packagePattern struct {
	prefix string
	tree   bool
}

// Rule is the stable metadata for consumer-owned interface policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryAPI,
	Severity:          shared.SeverityWarning,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Provider-owned interfaces couple consumers to abstractions they do not require.",
	Remediation:       "Declare the smallest interface in the consuming package and accept it at the use site.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"interface_provider_packages": {
			Type:        shared.ConfigurationArray,
			Description: "Provider package trees forbidden from declaring exported value interfaces.",
		},
	}},
}

// Analyzer is inactive until provider package trees are configured.
var Analyzer, _ = New(Options{})

// New validates provider package patterns and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	packages := make([]packagePattern, 0, len(options.Packages))
	for _, configured := range options.Packages {
		pattern, err := compilePattern(configured)
		if err != nil {
			return nil, fmt.Errorf("interface provider package pattern: %w", err)
		}
		for _, existing := range packages {
			if pattern.overlaps(existing) {
				return nil, fmt.Errorf(
					"interface provider package %q overlaps another policy",
					configured,
				)
			}
		}
		packages = append(packages, pattern)
	}

	return &analysis.Analyzer{
		Name: "interfaceplacement",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if !governs(packages, pass.Pkg.Path()) {
				return nil, nil
			}
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					general, ok := declaration.(*ast.GenDecl)
					if !ok || general.Tok != token.TYPE {
						continue
					}
					for _, specification := range general.Specs {
						typeSpec := specification.(*ast.TypeSpec)
						if !ast.IsExported(typeSpec.Name.Name) ||
							!isValueInterface(pass.TypesInfo.Defs[typeSpec.Name]) {
							continue
						}
						pass.Reportf(
							typeSpec.Name.Pos(),
							"%s: exported interface %s is declared in a configured provider package",
							ruleID,
							typeSpec.Name.Name,
						)
					}
				}
			}
			return nil, nil
		},
	}, nil
}

func isValueInterface(object types.Object) bool {
	if object == nil {
		return false
	}
	underlying := types.Unalias(object.Type()).Underlying()
	contract, ok := underlying.(*types.Interface)
	return ok && contract.Complete().IsMethodSet()
}

func governs(packages []packagePattern, packagePath string) bool {
	for _, pattern := range packages {
		if pattern.matches(packagePath) {
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
