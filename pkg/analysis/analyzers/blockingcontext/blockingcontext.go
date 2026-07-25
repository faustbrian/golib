// Package blockingcontext requires context on configured blocking APIs.
package blockingcontext

import (
	"errors"
	"go/ast"
	"go/types"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "context/blocking-api-context"

// Policy names exported blocking functions in one exact package.
type Policy struct {
	Package   string
	Functions []string
}

// Options configures blocking public API contracts.
type Options struct {
	Policies []Policy
}

// Rule is the stable metadata for blocking API context policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryContext,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Blocking APIs without context cannot propagate cancellation or deadlines.",
	Remediation:       "Accept context.Context and propagate it through blocking operations.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"policies": {
			Type:        shared.ConfigurationArray,
			Description: "Exact packages and exported blocking API symbols.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates public symbol policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	configured := make(map[string]map[string]struct{}, len(options.Policies))
	for _, policy := range options.Policies {
		if policy.Package == "" || path.Clean(policy.Package) != policy.Package ||
			strings.Contains(policy.Package, "*") {
			return nil, errors.New("blocking API policy requires an exact clean package path")
		}
		if len(policy.Functions) == 0 {
			return nil, errors.New("blocking API policy requires at least one function")
		}
		if configured[policy.Package] == nil {
			configured[policy.Package] = make(map[string]struct{}, len(policy.Functions))
		}
		for _, symbol := range policy.Functions {
			if !validSymbol(symbol) {
				return nil, errors.New("blocking API symbols must be exported Function or Type.Method names")
			}
			configured[policy.Package][symbol] = struct{}{}
		}
	}

	return &analysis.Analyzer{
		Name: "blockingcontext",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			symbols := configured[pass.Pkg.Path()]
			if len(symbols) == 0 {
				return nil, nil
			}
			contextType, contextInterface := contextContract(pass)
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if !ok {
						continue
					}
					object, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
					if !ok {
						continue
					}
					signature := object.Type().(*types.Signature)
					symbol := functionSymbol(object, signature)
					if _, required := symbols[symbol]; !required ||
						hasContext(signature, contextType, contextInterface) {
						continue
					}
					pass.Reportf(
						function.Name.Pos(),
						"%s: configured blocking API %s requires context.Context",
						ruleID,
						symbol,
					)
				}
			}
			return nil, nil
		},
	}, nil
}

func validSymbol(symbol string) bool {
	parts := strings.Split(symbol, ".")
	if len(parts) == 1 {
		return ast.IsExported(parts[0])
	}
	return len(parts) == 2 && ast.IsExported(parts[0]) && ast.IsExported(parts[1])
}

func contextContract(pass *analysis.Pass) (types.Type, *types.Interface) {
	for _, imported := range pass.Pkg.Imports() {
		if imported.Path() != "context" {
			continue
		}
		object := imported.Scope().Lookup("Context")
		if object == nil {
			return nil, nil
		}
		contract, ok := object.Type().Underlying().(*types.Interface)
		if !ok {
			return nil, nil
		}
		return object.Type(), contract.Complete()
	}

	return nil, nil
}

func functionSymbol(function *types.Func, signature *types.Signature) string {
	if signature.Recv() == nil {
		return function.Name()
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := receiver.(*types.Named); ok {
		return named.Obj().Name() + "." + function.Name()
	}

	return function.Name()
}

func hasContext(
	signature *types.Signature,
	contextType types.Type,
	contextInterface *types.Interface,
) bool {
	if contextInterface == nil {
		return false
	}
	for index := range signature.Params().Len() {
		parameter := signature.Params().At(index).Type()
		if types.AssignableTo(parameter, contextType) ||
			types.Implements(parameter, contextInterface) {
			return true
		}
	}

	return false
}
