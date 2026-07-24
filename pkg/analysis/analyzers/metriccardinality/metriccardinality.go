// Package metriccardinality detects configured high-cardinality typed values
// passed to configured metric label positions.
package metriccardinality

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
	ruleID          = "observability/high-cardinality-label"
	labelNameRuleID = "observability/dynamic-label-name"
)

// HighCardinalityType identifies a named type that must not become a metric
// label without an explicit reviewed transformation.
type HighCardinalityType struct {
	Package string
	Name    string
}

// AttackerControlledType identifies a named type proven to carry untrusted
// input that must not control metric label names.
type AttackerControlledType = HighCardinalityType

// Sink identifies metric label argument positions on an exact callable.
type Sink struct {
	Package      string
	Symbol       string
	Arguments    []int
	VariadicFrom *int
}

// Options configures high-cardinality types and metric label sinks.
type Options struct {
	HighCardinalityTypes []HighCardinalityType
	Sinks                []Sink
}

// LabelNameOptions configures attacker-controlled types and metric label-name
// sink positions.
type LabelNameOptions struct {
	AttackerControlledTypes []AttackerControlledType
	Sinks                   []Sink
}

type symbolKey struct {
	packagePath string
	symbol      string
}

type compiledSink struct {
	arguments    map[int]struct{}
	variadicFrom int
}

// Rule is the stable metadata for typed metric-cardinality policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryObservability,
	Severity:          shared.SeverityWarning,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "High-cardinality metric labels create unbounded time series and observability cost.",
	Remediation:       "Replace the value with a bounded category or remove it from metric labels.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"metric_label_types": {
			Type:        shared.ConfigurationArray,
			Description: "Exact named types carrying high-cardinality label values.",
		},
		"metric_label_sinks": {
			Type:        shared.ConfigurationArray,
			Description: "Exact metric callables and zero-based label argument positions.",
		},
	}},
}

// LabelNameRule is the stable metadata for attacker-controlled metric label
// names.
var LabelNameRule = shared.Rule{
	ID:                labelNameRuleID,
	Category:          shared.CategoryObservability,
	Severity:          shared.SeverityWarning,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Attacker-controlled metric label names create unbounded metric schemas and time series.",
	Remediation:       "Map the input to a fixed allowlist of label names or use it only as a bounded label value.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"metric_label_name_types": {
			Type:        shared.ConfigurationArray,
			Description: "Exact named types carrying attacker-controlled label names.",
		},
		"metric_label_name_sinks": {
			Type:        shared.ConfigurationArray,
			Description: "Exact metric callables and zero-based label-name positions.",
		},
	}},
}

// Analyzer is inactive until both types and sinks are configured.
var Analyzer, _ = New(Options{})

// LabelNameAnalyzer is inactive until both attacker-controlled types and
// label-name sinks are configured.
var LabelNameAnalyzer, _ = NewLabelName(LabelNameOptions{})

// New validates typed metric policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	return newMetricAnalyzer(
		"metriccardinality",
		Rule,
		options.HighCardinalityTypes,
		options.Sinks,
		reportCardinalityCall,
	)
}

// NewLabelName validates typed label-name policy and constructs an analyzer.
func NewLabelName(options LabelNameOptions) (*analysis.Analyzer, error) {
	return newMetricAnalyzer(
		"metriclabelname",
		LabelNameRule,
		options.AttackerControlledTypes,
		options.Sinks,
		reportLabelNameCall,
	)
}

type callReporter func(
	*analysis.Pass,
	*ast.CallExpr,
	map[symbolKey]struct{},
	map[symbolKey]compiledSink,
)

func newMetricAnalyzer(
	name string,
	rule shared.Rule,
	configuredTypes []HighCardinalityType,
	configuredSinks []Sink,
	report callReporter,
) (*analysis.Analyzer, error) {
	highCardinality := make(map[symbolKey]struct{}, len(configuredTypes))
	for _, configuredType := range configuredTypes {
		if !exactPackage(configuredType.Package) ||
			!token.IsIdentifier(configuredType.Name) || configuredType.Name == "_" {
			return nil, errors.New(
				"metric label types require an exact package and type name",
			)
		}
		key := symbolKey{packagePath: configuredType.Package, symbol: configuredType.Name}
		if _, duplicate := highCardinality[key]; duplicate {
			return nil, errors.New("metric label type policy contains a duplicate type")
		}
		highCardinality[key] = struct{}{}
	}

	sinks := make(map[symbolKey]compiledSink, len(configuredSinks))
	for _, sink := range configuredSinks {
		if !exactPackage(sink.Package) || !validSymbol(sink.Symbol) {
			return nil, errors.New(
				"metric label sinks require an exact package and Function or Type.Method symbol",
			)
		}
		if len(sink.Arguments) == 0 && sink.VariadicFrom == nil {
			return nil, errors.New("metric label sink requires at least one argument position")
		}
		key := symbolKey{packagePath: sink.Package, symbol: sink.Symbol}
		if _, duplicate := sinks[key]; duplicate {
			return nil, errors.New("metric label sink policy contains a duplicate callable")
		}
		compiled := compiledSink{
			arguments:    make(map[int]struct{}, len(sink.Arguments)),
			variadicFrom: -1,
		}
		for _, argument := range sink.Arguments {
			if argument < 0 {
				return nil, errors.New("metric label argument positions must be non-negative")
			}
			if _, duplicate := compiled.arguments[argument]; duplicate {
				return nil, errors.New("metric label sink contains a duplicate argument position")
			}
			compiled.arguments[argument] = struct{}{}
		}
		if sink.VariadicFrom != nil {
			if *sink.VariadicFrom < 0 {
				return nil, errors.New("metric label variadic position must be non-negative")
			}
			compiled.variadicFrom = *sink.VariadicFrom
		}
		sinks[key] = compiled
	}

	return &analysis.Analyzer{
		Name: name,
		Doc:  rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(highCardinality) == 0 || len(sinks) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok {
						report(pass, call, highCardinality, sinks)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
}

func reportCardinalityCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	highCardinality map[symbolKey]struct{},
	sinks map[symbolKey]compiledSink,
) {
	reportTypedCall(pass, call, highCardinality, sinks, ruleID, "metric label")
}

func reportLabelNameCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	highCardinality map[symbolKey]struct{},
	sinks map[symbolKey]compiledSink,
) {
	reportTypedCall(
		pass,
		call,
		highCardinality,
		sinks,
		labelNameRuleID,
		"metric label name",
	)
}

func reportTypedCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	highCardinality map[symbolKey]struct{},
	sinks map[symbolKey]compiledSink,
	diagnosticRuleID string,
	sinkDescription string,
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
	for index, argument := range call.Args {
		_, exact := sink.arguments[index]
		if !exact && (sink.variadicFrom < 0 || index < sink.variadicFrom) {
			continue
		}
		evidenceType := highCardinalityEvidence(pass, argument, highCardinality)
		if evidenceType == nil {
			continue
		}
		argumentNumber := index + 1
		pass.Reportf(
			argument.Pos(),
			"%s: %s flows to %s %s.%s argument %d",
			diagnosticRuleID,
			types.TypeString(evidenceType, func(pkg *types.Package) string {
				return pkg.Path()
			}),
			sinkDescription,
			called.Pkg().Path(),
			symbol,
			argumentNumber,
		)
	}
}

func highCardinalityEvidence(
	pass *analysis.Pass,
	expression ast.Expr,
	highCardinality map[symbolKey]struct{},
) types.Type {
	valueType := pass.TypesInfo.TypeOf(expression)
	if containsHighCardinality(valueType, highCardinality) {
		return valueType
	}
	conversion, ok := expression.(*ast.CallExpr)
	if !ok || len(conversion.Args) != 1 ||
		!pass.TypesInfo.Types[conversion.Fun].IsType() {
		return nil
	}

	return highCardinalityEvidence(pass, conversion.Args[0], highCardinality)
}

func containsHighCardinality(
	value types.Type,
	highCardinality map[symbolKey]struct{},
) bool {
	value = types.Unalias(value)
	switch value := value.(type) {
	case *types.Pointer:
		return containsHighCardinality(value.Elem(), highCardinality)
	case *types.Slice:
		return containsHighCardinality(value.Elem(), highCardinality)
	case *types.Array:
		return containsHighCardinality(value.Elem(), highCardinality)
	case *types.Map:
		return containsHighCardinality(value.Key(), highCardinality) ||
			containsHighCardinality(value.Elem(), highCardinality)
	case *types.Named:
		object := value.Obj()
		if object.Pkg() == nil {
			return false
		}
		_, configured := highCardinality[symbolKey{
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

func exactPackage(packagePath string) bool {
	return packagePath != "" && packagePath != "." &&
		!strings.HasPrefix(packagePath, "/") && path.Clean(packagePath) == packagePath &&
		!strings.Contains(packagePath, "*") && !strings.Contains(packagePath, "...")
}

func validSymbol(symbol string) bool {
	parts := strings.Split(symbol, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !token.IsIdentifier(part) || part == "_" {
			return false
		}
	}
	return true
}
