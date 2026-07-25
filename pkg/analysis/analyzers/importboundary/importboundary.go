// Package importboundary enforces configured package dependency direction.
package importboundary

import (
	"errors"
	"fmt"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "architecture/import-boundary"

// Policy defines imports denied for one package tree.
type Policy struct {
	Package      string
	DenyImports  []string
	AllowImports []string
}

// RestrictedDependency defines a package tree that only approved package
// trees may import. It models backend clients and other adapter-owned APIs.
type RestrictedDependency struct {
	Package         string
	AllowedPackages []string
}

// PackageClass assigns an exact package tree to a layer and bounded context.
type PackageClass struct {
	Package string
	Layer   string
	Context string
}

// Direction names one layer or context and the other names it may import.
// Imports within the same name are always allowed.
type Direction struct {
	Name      string
	MayImport []string
}

// Options configures all repository dependency boundaries.
type Options struct {
	Policies               []Policy
	RestrictedDependencies []RestrictedDependency
	Packages               []PackageClass
	Layers                 []Direction
	Contexts               []Direction
}

type packagePattern struct {
	prefix string
	tree   bool
}

type compiledPolicy struct {
	packagePattern packagePattern
	deny           []packagePattern
	allow          []packagePattern
}

type compiledClass struct {
	packagePattern packagePattern
	layer          string
	context        string
}

type compiledRestriction struct {
	packagePattern packagePattern
	allowed        []packagePattern
}

// Rule is the stable metadata for dependency direction policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryArchitecture,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Boundary bypasses couple inner packages to forbidden implementation details.",
	Remediation:       "Move the dependency behind an approved port or adapter.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"policies": {
			Type:        shared.ConfigurationArray,
			Description: "Package trees with explicit denied and allowed imports.",
		},
		"restricted_dependencies": {
			Type:        shared.ConfigurationArray,
			Description: "Backend package trees and the adapter trees allowed to import them.",
		},
		"packages": {
			Type:        shared.ConfigurationArray,
			Description: "Non-overlapping package trees assigned to layers and contexts.",
		},
		"layers": {
			Type:        shared.ConfigurationArray,
			Description: "Named layers and the other layers each may import.",
		},
		"contexts": {
			Type:        shared.ConfigurationArray,
			Description: "Named bounded contexts and the other contexts each may import.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates path semantics and constructs a configured analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	policies := make([]compiledPolicy, 0, len(options.Policies))
	for _, policy := range options.Policies {
		compiled, err := compilePolicy(policy)
		if err != nil {
			return nil, err
		}
		for _, existing := range policies {
			if compiled.packagePattern.overlaps(existing.packagePattern) {
				return nil, fmt.Errorf(
					"package boundary %q overlaps another boundary",
					policy.Package,
				)
			}
		}
		policies = append(policies, compiled)
	}
	restrictions, err := compileRestrictions(options.RestrictedDependencies)
	if err != nil {
		return nil, err
	}
	layers, err := compileDirections("layer", options.Layers)
	if err != nil {
		return nil, err
	}
	contexts, err := compileDirections("context", options.Contexts)
	if err != nil {
		return nil, err
	}
	classes, err := compileClasses(options.Packages, layers, contexts)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "importboundary",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, imported := range file.Imports {
					importPath, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						continue
					}
					if reportExplicit(pass, imported.Path.Pos(), importPath, policies) {
						continue
					}
					if reportRestricted(pass, imported.Path.Pos(), importPath, restrictions) {
						continue
					}
					reportDirection(
						pass,
						imported.Path.Pos(),
						importPath,
						classes,
						layers,
						contexts,
					)
				}
			}
			return nil, nil
		},
	}, nil
}

func compileRestrictions(
	restrictions []RestrictedDependency,
) ([]compiledRestriction, error) {
	compiled := make([]compiledRestriction, 0, len(restrictions))
	for _, restriction := range restrictions {
		packagePath, err := compilePattern(restriction.Package)
		if err != nil {
			return nil, fmt.Errorf("restricted dependency pattern: %w", err)
		}
		if len(restriction.AllowedPackages) == 0 {
			return nil, errors.New("restricted dependency requires at least one approved adapter")
		}
		for _, existing := range compiled {
			if packagePath.overlaps(existing.packagePattern) {
				return nil, fmt.Errorf(
					"restricted dependency %q overlaps another dependency",
					restriction.Package,
				)
			}
		}
		result := compiledRestriction{packagePattern: packagePath}
		for _, allowed := range restriction.AllowedPackages {
			pattern, err := compilePattern(allowed)
			if err != nil {
				return nil, fmt.Errorf("approved adapter pattern: %w", err)
			}
			for _, existing := range result.allowed {
				if pattern.overlaps(existing) {
					return nil, fmt.Errorf(
						"approved adapter %q overlaps another adapter",
						allowed,
					)
				}
			}
			result.allowed = append(result.allowed, pattern)
		}
		compiled = append(compiled, result)
	}

	return compiled, nil
}

func compileDirections(
	kind string,
	directions []Direction,
) (map[string]map[string]struct{}, error) {
	knownNames := make(map[string]struct{}, len(directions))
	for _, direction := range directions {
		if !validName(direction.Name) {
			return nil, fmt.Errorf("%s name must be lower-kebab-case", kind)
		}
		if _, duplicate := knownNames[direction.Name]; duplicate {
			return nil, fmt.Errorf("duplicate %s %q", kind, direction.Name)
		}
		knownNames[direction.Name] = struct{}{}
	}
	compiled := make(map[string]map[string]struct{}, len(directions))
	for _, direction := range directions {
		allowed := make(map[string]struct{}, len(direction.MayImport))
		for _, target := range direction.MayImport {
			if _, known := knownNames[target]; !known {
				return nil, fmt.Errorf("%s %q imports unknown %s %q",
					kind, direction.Name, kind, target)
			}
			if target == direction.Name {
				return nil, fmt.Errorf("%s %q redundantly imports itself", kind, direction.Name)
			}
			if _, duplicate := allowed[target]; duplicate {
				return nil, fmt.Errorf("%s %q contains duplicate import %q",
					kind, direction.Name, target)
			}
			allowed[target] = struct{}{}
		}
		compiled[direction.Name] = allowed
	}
	if cycle := directionCycle(compiled); cycle != nil {
		return nil, fmt.Errorf("%s dependency cycle: %s", kind, strings.Join(cycle, " -> "))
	}

	return compiled, nil
}

func directionCycle(directions map[string]map[string]struct{}) []string {
	names := make([]string, 0, len(directions))
	for name := range directions {
		names = append(names, name)
	}
	sort.Strings(names)

	const (
		unvisited uint8 = iota
		visiting
		visited
	)
	states := make(map[string]uint8, len(directions))
	stack := make([]string, 0, len(directions))
	stackIndex := make(map[string]int, len(directions))
	var visit func(string) []string
	visit = func(name string) []string {
		states[name] = visiting
		stackIndex[name] = len(stack)
		stack = append(stack, name)

		targets := make([]string, 0, len(directions[name]))
		for target := range directions[name] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			switch states[target] {
			case unvisited:
				if cycle := visit(target); cycle != nil {
					return cycle
				}
			case visiting:
				start := stackIndex[target]
				cycle := append([]string(nil), stack[start:]...)
				return append(cycle, target)
			}
		}

		stack = stack[:len(stack)-1]
		delete(stackIndex, name)
		states[name] = visited
		return nil
	}
	for _, name := range names {
		if states[name] == unvisited {
			if cycle := visit(name); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func compileClasses(
	classes []PackageClass,
	layers map[string]map[string]struct{},
	contexts map[string]map[string]struct{},
) ([]compiledClass, error) {
	compiled := make([]compiledClass, 0, len(classes))
	for _, class := range classes {
		pattern, err := compilePattern(class.Package)
		if err != nil {
			return nil, fmt.Errorf("classified package pattern: %w", err)
		}
		if class.Layer == "" && class.Context == "" {
			return nil, errors.New("classified package requires a layer or context")
		}
		if class.Layer != "" {
			if _, known := layers[class.Layer]; !known {
				return nil, fmt.Errorf("package %q uses unknown layer %q", class.Package, class.Layer)
			}
		}
		if class.Context != "" {
			if _, known := contexts[class.Context]; !known {
				return nil, fmt.Errorf("package %q uses unknown context %q", class.Package, class.Context)
			}
		}
		for _, other := range compiled {
			if pattern.overlaps(other.packagePattern) {
				return nil, fmt.Errorf("package classification %q overlaps another classification",
					class.Package)
			}
		}
		compiled = append(compiled, compiledClass{
			packagePattern: pattern,
			layer:          class.Layer,
			context:        class.Context,
		})
	}

	return compiled, nil
}

func validName(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' ||
		value[len(value)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' {
			previousHyphen = false
			continue
		}
		if character != '-' || previousHyphen {
			return false
		}
		previousHyphen = true
	}
	return true
}

func reportExplicit(
	pass *analysis.Pass,
	position token.Pos,
	importPath string,
	policies []compiledPolicy,
) bool {
	for _, policy := range policies {
		if !policy.packagePattern.matches(pass.Pkg.Path()) ||
			!policy.forbids(importPath) {
			continue
		}
		pass.Reportf(
			position,
			"%s: %s must not import %s",
			ruleID,
			pass.Pkg.Path(),
			importPath,
		)
		return true
	}

	return false
}

func reportRestricted(
	pass *analysis.Pass,
	position token.Pos,
	importPath string,
	restrictions []compiledRestriction,
) bool {
	for _, restriction := range restrictions {
		if !restriction.packagePattern.matches(importPath) {
			continue
		}
		for _, allowed := range restriction.allowed {
			if allowed.matches(pass.Pkg.Path()) {
				return false
			}
		}
		pass.Reportf(
			position,
			"%s: %s may only be imported by an approved adapter",
			ruleID,
			importPath,
		)
		return true
	}

	return false
}

func reportDirection(
	pass *analysis.Pass,
	position token.Pos,
	importPath string,
	classes []compiledClass,
	layers map[string]map[string]struct{},
	contexts map[string]map[string]struct{},
) {
	source, sourceFound := classify(classes, pass.Pkg.Path())
	target, targetFound := classify(classes, importPath)
	if !sourceFound || !targetFound {
		return
	}
	if !allows(layers, source.layer, target.layer) {
		pass.Reportf(
			position,
			"%s: %s must not import %s across layer %s -> %s",
			ruleID,
			pass.Pkg.Path(),
			importPath,
			source.layer,
			target.layer,
		)
		return
	}
	if !allows(contexts, source.context, target.context) {
		pass.Reportf(
			position,
			"%s: %s must not import %s across context %s -> %s",
			ruleID,
			pass.Pkg.Path(),
			importPath,
			source.context,
			target.context,
		)
	}
}

func classify(classes []compiledClass, packagePath string) (compiledClass, bool) {
	for _, class := range classes {
		if class.packagePattern.matches(packagePath) {
			return class, true
		}
	}

	return compiledClass{}, false
}

func allows(
	directions map[string]map[string]struct{},
	source string,
	target string,
) bool {
	if source == "" || target == "" || source == target {
		return true
	}
	_, allowed := directions[source][target]
	return allowed
}

func compilePolicy(policy Policy) (compiledPolicy, error) {
	packagePath, err := compilePattern(policy.Package)
	if err != nil {
		return compiledPolicy{}, fmt.Errorf("package pattern: %w", err)
	}
	if len(policy.DenyImports) == 0 {
		return compiledPolicy{}, errors.New("import boundary requires at least one denied import")
	}
	compiled := compiledPolicy{packagePattern: packagePath}
	for _, denied := range policy.DenyImports {
		pattern, err := compilePattern(denied)
		if err != nil {
			return compiledPolicy{}, fmt.Errorf("denied import pattern: %w", err)
		}
		compiled.deny = append(compiled.deny, pattern)
	}
	for _, allowed := range policy.AllowImports {
		pattern, err := compilePattern(allowed)
		if err != nil {
			return compiledPolicy{}, fmt.Errorf("allowed import pattern: %w", err)
		}
		compiled.allow = append(compiled.allow, pattern)
	}

	return compiled, nil
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

func (policy compiledPolicy) forbids(importPath string) bool {
	for _, allowed := range policy.allow {
		if allowed.matches(importPath) {
			return false
		}
	}
	for _, denied := range policy.deny {
		if denied.matches(importPath) {
			return true
		}
	}

	return false
}
