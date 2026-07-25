// Package sensitivesink detects typed sensitive values passed to configured sinks.
package sensitivesink

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

const ruleID = "security/sensitive-sink"

// SensitiveType identifies one named type carrying secret-bearing data.
type SensitiveType struct {
	Package string
	Name    string
}

// Sink identifies sensitive argument positions on one exact callable.
type Sink struct {
	Package         string
	Symbol          string
	Arguments       []int
	VariadicFrom    *int
	AllowedPackages []string
}

// Options configures sensitive types and output sinks.
type Options struct {
	SensitiveTypes []SensitiveType
	Sinks          []Sink
}

type symbolKey struct {
	packagePath string
	symbol      string
}

type compiledSink struct {
	arguments    map[int]struct{}
	variadicFrom int
	allowed      map[string]struct{}
}

// Rule is the stable metadata for typed sensitive-value handling.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategorySecurity,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Sensitive typed values can leak through logs, telemetry, errors, formatting, and URLs.",
	Remediation:       "Pass an explicitly redacted representation or remove the value from the configured sink.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"sensitive_types": {
			Type:        shared.ConfigurationArray,
			Description: "Exact named types carrying secret-bearing values.",
		},
		"sinks": {
			Type:        shared.ConfigurationArray,
			Description: "Exact callables and sensitive argument positions.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates typed data policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	sensitive := make(map[symbolKey]struct{}, len(options.SensitiveTypes))
	for _, configuredType := range options.SensitiveTypes {
		if !exactPackage(configuredType.Package) || !token.IsIdentifier(configuredType.Name) {
			return nil, errors.New("sensitive types require an exact package and type name")
		}
		key := symbolKey{packagePath: configuredType.Package, symbol: configuredType.Name}
		if _, duplicate := sensitive[key]; duplicate {
			return nil, errors.New("sensitive type policy contains a duplicate type")
		}
		sensitive[key] = struct{}{}
	}

	sinks := make(map[symbolKey]compiledSink, len(options.Sinks))
	for _, sink := range options.Sinks {
		if !exactPackage(sink.Package) || !validSymbol(sink.Symbol) {
			return nil, errors.New("sinks require an exact package and Function or Type.Method symbol")
		}
		if len(sink.Arguments) == 0 && sink.VariadicFrom == nil {
			return nil, errors.New("sink policy requires at least one argument position")
		}
		key := symbolKey{packagePath: sink.Package, symbol: sink.Symbol}
		if _, duplicate := sinks[key]; duplicate {
			return nil, errors.New("sink policy contains a duplicate callable")
		}
		compiled := compiledSink{
			arguments:    make(map[int]struct{}, len(sink.Arguments)),
			variadicFrom: -1,
			allowed:      make(map[string]struct{}, len(sink.AllowedPackages)),
		}
		for _, argument := range sink.Arguments {
			if argument < 0 {
				return nil, errors.New("sink argument positions must be non-negative")
			}
			if _, duplicate := compiled.arguments[argument]; duplicate {
				return nil, errors.New("sink policy contains a duplicate argument position")
			}
			compiled.arguments[argument] = struct{}{}
		}
		if sink.VariadicFrom != nil {
			if *sink.VariadicFrom < 0 {
				return nil, errors.New("sink variadic position must be non-negative")
			}
			compiled.variadicFrom = *sink.VariadicFrom
		}
		for _, packagePath := range sink.AllowedPackages {
			if !exactPackage(packagePath) {
				return nil, errors.New("sink exceptions require exact package paths")
			}
			compiled.allowed[packagePath] = struct{}{}
		}
		sinks[key] = compiled
	}

	return &analysis.Analyzer{
		Name: "sensitivesink",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(sensitive) == 0 || len(sinks) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok {
						reportCall(pass, call, sensitive, sinks)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
}

func exactPackage(packagePath string) bool {
	return packagePath != "" && path.Clean(packagePath) == packagePath &&
		!strings.Contains(packagePath, "*")
}

func validSymbol(symbol string) bool {
	parts := strings.Split(symbol, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !token.IsIdentifier(part) {
			return false
		}
	}
	return true
}

func reportCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	sensitive map[symbolKey]struct{},
	sinks map[symbolKey]compiledSink,
) {
	object := calledObject(pass, call.Fun)
	called, ok := object.(*types.Func)
	if !ok || called.Pkg() == nil {
		return
	}
	symbol := functionSymbol(called)
	sink, configured := sinks[symbolKey{
		packagePath: called.Pkg().Path(), symbol: symbol,
	}]
	if !configured {
		return
	}
	if _, allowed := sink.allowed[pass.Pkg.Path()]; allowed {
		return
	}
	for index, argument := range call.Args {
		_, exact := sink.arguments[index]
		if !exact && (sink.variadicFrom < 0 || index < sink.variadicFrom) {
			continue
		}
		argumentType := pass.TypesInfo.TypeOf(argument)
		if !containsSensitive(argumentType, sensitive) {
			continue
		}
		pass.Reportf(
			argument.Pos(),
			"%s: %s flows to %s.%s argument %d",
			ruleID,
			types.TypeString(argumentType, func(pkg *types.Package) string {
				return pkg.Path()
			}),
			called.Pkg().Path(),
			symbol,
			displayArgument(index),
		)
	}
}

func displayArgument(index int) int {
	return index + 1
}

func containsSensitive(value types.Type, sensitive map[symbolKey]struct{}) bool {
	value = types.Unalias(value)
	switch value := value.(type) {
	case *types.Pointer:
		return containsSensitive(value.Elem(), sensitive)
	case *types.Slice:
		return containsSensitive(value.Elem(), sensitive)
	case *types.Array:
		return containsSensitive(value.Elem(), sensitive)
	case *types.Map:
		return containsSensitive(value.Key(), sensitive) ||
			containsSensitive(value.Elem(), sensitive)
	case *types.Named:
		object := value.Obj()
		if object.Pkg() == nil {
			return false
		}
		_, configured := sensitive[symbolKey{
			packagePath: object.Pkg().Path(), symbol: object.Name(),
		}]
		return configured
	default:
		return false
	}
}

func calledObject(pass *analysis.Pass, expression ast.Expr) types.Object {
	switch expression := expression.(type) {
	case *ast.IndexExpr:
		return calledObject(pass, expression.X)
	case *ast.IndexListExpr:
		return calledObject(pass, expression.X)
	case *ast.Ident:
		return pass.TypesInfo.Uses[expression]
	case *ast.SelectorExpr:
		return pass.TypesInfo.Uses[expression.Sel]
	default:
		return nil
	}
}

func functionSymbol(function *types.Func) string {
	symbol := function.Name()
	signature := function.Type().(*types.Signature)
	if signature.Recv() != nil {
		receiver := signature.Recv().Type()
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = pointer.Elem()
		}
		if named, ok := receiver.(*types.Named); ok {
			symbol = named.Obj().Name() + "." + symbol
		}
	}
	return symbol
}
