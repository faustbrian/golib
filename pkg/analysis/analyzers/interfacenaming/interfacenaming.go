// Package interfacenaming enforces configured names for exported value interfaces.
package interfacenaming

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const (
	ruleID              = "api/interface-naming"
	maxPolicies         = 1024
	maxAllowedNamesEach = 1024
)

// Policy configures one non-overlapping package tree's interface names.
type Policy struct {
	Package        string
	RequiredPrefix string
	RequiredSuffix string
	AllowedNames   []string
}

// Options configures interface naming policies.
type Options struct {
	Policies []Policy
}

type compiledPolicy struct {
	packagePath string
	tree        bool
	prefix      string
	suffix      string
	allowed     map[string]struct{}
}

// Rule is the stable metadata for configured interface naming policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryAPI,
	Severity:          shared.SeverityWarning,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Organization interface names communicate stable architectural roles at API boundaries.",
	Remediation:       "Rename the interface to match its configured role or add an exact reviewed compatibility name.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"interface_names": {
			Type:        shared.ConfigurationArray,
			Description: "Package trees with required interface prefixes, suffixes, and exact compatibility names.",
		},
	}},
}

// Analyzer is inactive until interface naming policy is configured.
var Analyzer, _ = New(Options{})

// New constructs an independently configured analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	if len(options.Policies) > maxPolicies {
		return nil, errors.New("interface naming policy exceeds 1024 package entries")
	}
	policies := make([]compiledPolicy, 0, len(options.Policies))
	for _, configured := range options.Policies {
		compiled, err := compilePolicy(configured)
		if err != nil {
			return nil, err
		}
		for _, existing := range policies {
			if policiesOverlap(existing, compiled) {
				return nil, errors.New("interface naming package policies overlap")
			}
		}
		policies = append(policies, compiled)
	}

	return &analysis.Analyzer{
		Name: "interfacenaming",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			policy, configured := packagePolicy(pass.Pkg.Path(), policies)
			if !configured {
				return nil, nil
			}
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					reportDeclaration(pass, declaration, policy)
				}
			}
			return nil, nil
		},
	}, nil
}

func compilePolicy(configured Policy) (compiledPolicy, error) {
	packagePath, tree, ok := packagePattern(configured.Package)
	if !ok {
		return compiledPolicy{}, errors.New("interface naming policy requires an exact package or trailing /... tree")
	}
	if configured.RequiredPrefix == "" && configured.RequiredSuffix == "" {
		return compiledPolicy{}, errors.New("interface naming policy requires a prefix or suffix")
	}
	if configured.RequiredPrefix != "" && !exportedIdentifier(configured.RequiredPrefix) {
		return compiledPolicy{}, errors.New("interface naming prefix must be an exported identifier")
	}
	if configured.RequiredSuffix != "" && !exportedIdentifier(configured.RequiredSuffix) {
		return compiledPolicy{}, errors.New("interface naming suffix must be an exported identifier")
	}
	if len(configured.AllowedNames) > maxAllowedNamesEach {
		return compiledPolicy{}, errors.New("interface naming policy exceeds 1024 compatibility names")
	}
	allowed := make(map[string]struct{}, len(configured.AllowedNames))
	for _, name := range configured.AllowedNames {
		if !exportedIdentifier(name) {
			return compiledPolicy{}, errors.New("interface compatibility names must be exported identifiers")
		}
		if _, duplicate := allowed[name]; duplicate {
			return compiledPolicy{}, errors.New("interface naming policy contains a duplicate compatibility name")
		}
		allowed[name] = struct{}{}
	}

	return compiledPolicy{
		packagePath: packagePath,
		tree:        tree,
		prefix:      configured.RequiredPrefix,
		suffix:      configured.RequiredSuffix,
		allowed:     allowed,
	}, nil
}

func packagePattern(pattern string) (string, bool, bool) {
	tree := strings.HasSuffix(pattern, "/...")
	base := strings.TrimSuffix(pattern, "/...")
	if base == "" || base == "." || path.Clean(base) != base ||
		strings.ContainsAny(base, "*\\") || strings.Contains(base, "...") {
		return "", false, false
	}
	return base, tree, true
}

func exportedIdentifier(value string) bool {
	return token.IsIdentifier(value) && ast.IsExported(value)
}

func policiesOverlap(left, right compiledPolicy) bool {
	return left.packagePath == right.packagePath ||
		(left.tree && strings.HasPrefix(right.packagePath, left.packagePath+"/")) ||
		(right.tree && strings.HasPrefix(left.packagePath, right.packagePath+"/"))
}

func packagePolicy(packagePath string, policies []compiledPolicy) (compiledPolicy, bool) {
	for _, policy := range policies {
		if packagePath == policy.packagePath ||
			(policy.tree && strings.HasPrefix(packagePath, policy.packagePath+"/")) {
			return policy, true
		}
	}
	return compiledPolicy{}, false
}

func reportDeclaration(pass *analysis.Pass, declaration ast.Decl, policy compiledPolicy) {
	generic, ok := declaration.(*ast.GenDecl)
	if !ok || generic.Tok != token.TYPE {
		return
	}
	for _, specification := range generic.Specs {
		typeSpec := specification.(*ast.TypeSpec)
		if !typeSpec.Name.IsExported() || nameAllowed(typeSpec.Name.Name, policy) {
			continue
		}
		object, ok := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
		if !ok {
			continue
		}
		interfaceType, ok := types.Unalias(object.Type()).Underlying().(*types.Interface)
		if !ok || !interfaceType.Complete().IsMethodSet() {
			continue
		}
		pass.Reportf(typeSpec.Name.Pos(), "%s: exported interface %s %s", ruleID,
			typeSpec.Name.Name, requirement(policy))
	}
}

func nameAllowed(name string, policy compiledPolicy) bool {
	if _, allowed := policy.allowed[name]; allowed {
		return true
	}
	return (policy.prefix == "" || strings.HasPrefix(name, policy.prefix)) &&
		(policy.suffix == "" || strings.HasSuffix(name, policy.suffix))
}

func requirement(policy compiledPolicy) string {
	if policy.prefix != "" && policy.suffix != "" {
		return "must start with " + policy.prefix + " and end with " + policy.suffix
	}
	if policy.prefix != "" {
		return "must start with " + policy.prefix
	}
	return "must end with " + policy.suffix
}
