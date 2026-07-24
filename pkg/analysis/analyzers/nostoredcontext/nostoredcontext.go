// Package nostoredcontext forbids context lifecycle storage in structs.
package nostoredcontext

import (
	"go/ast"
	"go/types"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "context/no-stored-context"

// Options configures reviewed lifecycle owner packages.
type Options struct {
	AllowedPackages []string
}

// Rule is the stable metadata for context ownership policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryContext,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Struct storage obscures who owns context cancellation and lifetime.",
	Remediation:       "Pass context through each operation instead of retaining it.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"allowed_packages": {
			Type:        shared.ConfigurationArray,
			Description: "Exact packages with reviewed context ownership.",
		},
	}},
}

// Analyzer reports struct fields that semantically implement context.Context.
var Analyzer = New(Options{})

// New constructs an independently configured analyzer.
func New(options Options) *analysis.Analyzer {
	allowed := make(map[string]struct{}, len(options.AllowedPackages))
	for _, packagePath := range options.AllowedPackages {
		allowed[packagePath] = struct{}{}
	}

	return &analysis.Analyzer{
		Name: "nostoredcontext",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if _, ok := allowed[pass.Pkg.Path()]; ok {
				return nil, nil
			}
			contextType, contextInterface := resolveContext(pass)
			if contextInterface == nil {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					structure, ok := node.(*ast.StructType)
					if !ok {
						return true
					}
					for _, field := range structure.Fields.List {
						fieldType := pass.TypesInfo.TypeOf(field.Type)
						if fieldType == nil ||
							(!types.AssignableTo(fieldType, contextType) &&
								!types.Implements(fieldType, contextInterface)) {
							continue
						}
						pass.Reportf(
							field.Pos(),
							"%s: struct field stores a context lifecycle",
							ruleID,
						)
					}
					return true
				})
			}
			return nil, nil
		},
	}
}

func resolveContext(pass *analysis.Pass) (types.Type, *types.Interface) {
	for _, imported := range pass.Pkg.Imports() {
		if imported.Path() != "context" {
			continue
		}
		object := imported.Scope().Lookup("Context")
		if object == nil {
			return nil, nil
		}
		contextInterface, ok := object.Type().Underlying().(*types.Interface)
		if !ok {
			return nil, nil
		}

		return object.Type(), contextInterface.Complete()
	}

	return nil, nil
}
