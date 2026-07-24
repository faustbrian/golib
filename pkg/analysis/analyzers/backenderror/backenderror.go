// Package backenderror prevents configured backend errors from crossing
// exported API boundaries without an explicit classification step.
package backenderror

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

const (
	ruleID            = "api/backend-error-boundary"
	maximumTraceNodes = 256
)

// Flow identifies one result from an exact backend callable.
type Flow struct {
	Package string
	Symbol  string
	Result  int
}

// Passthrough identifies a callable result that preserves errors from exact
// argument positions.
type Passthrough struct {
	Package      string
	Symbol       string
	Result       int
	Arguments    []int
	VariadicFrom *int
}

// Options configures exported boundary packages and backend error provenance.
type Options struct {
	Boundaries   []string
	Sources      []Flow
	Passthroughs []Passthrough
}

type flowKey struct {
	packagePath string
	symbol      string
	result      int
}

type callableKey struct {
	packagePath string
	symbol      string
}

type compiledPassthrough struct {
	result       int
	arguments    map[int]struct{}
	variadicFrom int
}

type tracer struct {
	sources      map[flowKey]struct{}
	passthroughs map[callableKey]compiledPassthrough
	visited      map[ssa.Value]struct{}
	visitedCount int
	origins      map[flowKey]struct{}
}

// Rule is the stable metadata for backend error boundary policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryAPI,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Backend errors crossing public boundaries leak implementation contracts and unstable classification.",
	Remediation:       "Map the backend failure to a stable package-owned error before returning it.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"backend_error_boundaries": {
			Type:        shared.ConfigurationArray,
			Description: "Exact packages or trailing /... trees that own exported API boundaries.",
		},
		"backend_error_sources": {
			Type:        shared.ConfigurationArray,
			Description: "Exact backend callables and zero-based error result positions.",
		},
		"backend_error_passthroughs": {
			Type:        shared.ConfigurationArray,
			Description: "Exact wrapper results and argument positions that preserve backend errors.",
		},
	}},
}

// Analyzer is inactive until boundaries and sources are configured.
var Analyzer, _ = New(Options{})

// New validates backend error policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	boundaries, err := compileBoundaries(options.Boundaries)
	if err != nil {
		return nil, err
	}
	sources := make(map[flowKey]struct{}, len(options.Sources))
	for _, source := range options.Sources {
		if !exactPackage(source.Package) || !validSymbol(source.Symbol) || source.Result < 0 {
			return nil, errors.New("backend error sources require an exact callable and non-negative result")
		}
		key := flowKey{packagePath: source.Package, symbol: source.Symbol, result: source.Result}
		if _, duplicate := sources[key]; duplicate {
			return nil, errors.New("backend error policy contains a duplicate source result")
		}
		sources[key] = struct{}{}
	}
	passthroughs := make(map[callableKey]compiledPassthrough, len(options.Passthroughs))
	for _, configured := range options.Passthroughs {
		if !exactPackage(configured.Package) || !validSymbol(configured.Symbol) || configured.Result < 0 {
			return nil, errors.New("backend error passthroughs require an exact callable and non-negative result")
		}
		if len(configured.Arguments) == 0 && configured.VariadicFrom == nil {
			return nil, errors.New("backend error passthrough requires an argument position")
		}
		key := callableKey{packagePath: configured.Package, symbol: configured.Symbol}
		if _, duplicate := passthroughs[key]; duplicate {
			return nil, errors.New("backend error policy contains a duplicate passthrough")
		}
		compiled := compiledPassthrough{
			result:       configured.Result,
			arguments:    make(map[int]struct{}, len(configured.Arguments)),
			variadicFrom: -1,
		}
		for _, argument := range configured.Arguments {
			if argument < 0 {
				return nil, errors.New("backend error passthrough arguments must be non-negative")
			}
			if _, duplicate := compiled.arguments[argument]; duplicate {
				return nil, errors.New("backend error passthrough contains a duplicate argument")
			}
			compiled.arguments[argument] = struct{}{}
		}
		if configured.VariadicFrom != nil {
			if *configured.VariadicFrom < 0 {
				return nil, errors.New("backend error passthrough variadic position must be non-negative")
			}
			compiled.variadicFrom = *configured.VariadicFrom
		}
		passthroughs[key] = compiled
	}

	return &analysis.Analyzer{
		Name: "backenderror",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(boundaries) == 0 || len(sources) == 0 || !matchesPackage(pass.Pkg.Path(), boundaries) {
				return nil, nil
			}
			for _, function := range sourceFunctions(pass) {
				reportFunction(pass, function, sources, passthroughs)
			}
			return nil, nil
		},
	}, nil
}

func sourceFunctions(pass *analysis.Pass) []*ssa.Function {
	program := ssa.NewProgram(pass.Fset, 0)
	imports := append([]*types.Package(nil), pass.Pkg.Imports()...)
	slices.SortFunc(imports, func(left, right *types.Package) int {
		return strings.Compare(left.Path(), right.Path())
	})
	for _, imported := range imports {
		program.CreatePackage(imported, nil, nil, true)
	}
	ssaPackage := program.CreatePackage(pass.Pkg, pass.Files, pass.TypesInfo, false)
	ssaPackage.Build()

	objects := make([]*types.Func, 0)
	for _, object := range pass.TypesInfo.Defs {
		function, ok := object.(*types.Func)
		if ok && function.Pkg() == pass.Pkg && function.Exported() {
			objects = append(objects, function)
		}
	}
	slices.SortFunc(objects, func(left, right *types.Func) int {
		return cmp.Compare(left.Pos(), right.Pos())
	})
	functions := make([]*ssa.Function, 0, len(objects))
	for _, object := range objects {
		if function := program.FuncValue(object); function != nil {
			functions = append(functions, function)
		}
	}

	return functions
}

func reportFunction(
	pass *analysis.Pass,
	function *ssa.Function,
	sources map[flowKey]struct{},
	passthroughs map[callableKey]compiledPassthrough,
) {
	if !exportedFunction(function) {
		return
	}
	results := function.Signature.Results()
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			returned, ok := instruction.(*ssa.Return)
			if !ok {
				continue
			}
			for index, value := range returned.Results {
				if !isError(results.At(index).Type()) {
					continue
				}
				trace := tracer{
					sources: sources, passthroughs: passthroughs,
					visited: make(map[ssa.Value]struct{}), origins: make(map[flowKey]struct{}),
				}
				trace.value(value)
				ordered := make([]flowKey, 0, len(trace.origins))
				for origin := range trace.origins {
					ordered = append(ordered, origin)
				}
				sortFlows(ordered)
				for _, origin := range ordered {
					pass.Reportf(
						returned.Pos(),
						"%s: %s crosses exported boundary %s.%s",
						ruleID,
						flowDescription(origin),
						pass.Pkg.Path(),
						functionSymbol(function.Object().(*types.Func)),
					)
				}
			}
		}
	}
}

func (trace *tracer) value(value ssa.Value) {
	if value == nil || trace.visitedCount >= maximumTraceNodes {
		return
	}
	if _, visited := trace.visited[value]; visited {
		return
	}
	trace.visited[value] = struct{}{}
	trace.visitedCount++

	switch value := value.(type) {
	case *ssa.Call:
		trace.call(value.Common(), 0)
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			trace.call(call.Common(), value.Index)
		}
	case *ssa.ChangeInterface:
		trace.value(value.X)
	case *ssa.MakeInterface:
		trace.value(value.X)
	case *ssa.TypeAssert:
		trace.value(value.X)
	case *ssa.Slice:
		trace.value(value.X)
	case *ssa.Alloc:
		trace.storedValues(value)
	case *ssa.IndexAddr:
		trace.storedValues(value)
	case *ssa.Phi:
		for _, edge := range value.Edges {
			trace.value(edge)
		}
	}
}

func (trace *tracer) storedValues(address ssa.Value) {
	references := address.Referrers()
	if references == nil {
		return
	}
	for _, reference := range *references {
		switch reference := reference.(type) {
		case *ssa.IndexAddr:
			trace.value(reference)
		case *ssa.Store:
			if reference.Addr == address {
				trace.value(reference.Val)
			}
		}
	}
}

func (trace *tracer) call(call *ssa.CallCommon, result int) {
	callee := call.StaticCallee()
	if callee == nil || callee.Object() == nil {
		return
	}
	object := callee.Object().(*types.Func)
	key := callableKey{
		packagePath: object.Pkg().Path(),
		symbol:      functionSymbol(object),
	}
	source := flowKey{packagePath: key.packagePath, symbol: key.symbol, result: result}
	if _, configured := trace.sources[source]; configured {
		trace.origins[source] = struct{}{}
		return
	}
	passthrough, configured := trace.passthroughs[key]
	if !configured || passthrough.result != result {
		return
	}
	offset := 0
	if object.Type().(*types.Signature).Recv() != nil {
		offset = 1
	}
	for index, argument := range call.Args[offset:] {
		_, exact := passthrough.arguments[index]
		if exact || (passthrough.variadicFrom >= 0 && index >= passthrough.variadicFrom) {
			trace.value(argument)
		}
	}
}

func exportedFunction(function *ssa.Function) bool {
	object := function.Object().(*types.Func)
	signature := object.Type().(*types.Signature)
	if signature.Recv() == nil {
		return true
	}
	receiver := types.Unalias(signature.Recv().Type())
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = types.Unalias(pointer.Elem())
	}
	named, ok := receiver.(*types.Named)
	return ok && named.Obj().Exported()
}

func isError(value types.Type) bool {
	errorInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	return types.Implements(value, errorInterface)
}

func flowDescription(flow flowKey) string {
	return fmt.Sprintf("%s.%s result %d", flow.packagePath, flow.symbol, flow.result+1)
}

func sortFlows(flows []flowKey) {
	slices.SortFunc(flows, func(left, right flowKey) int {
		return strings.Compare(flowDescription(left), flowDescription(right))
	})
}

func compileBoundaries(patterns []string) ([]string, error) {
	compiled := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if !validPattern(pattern) {
			return nil, errors.New("backend error boundaries require exact packages or trailing /... trees")
		}
		for _, existing := range compiled {
			if patternsOverlap(existing, pattern) {
				return nil, errors.New("backend error boundary patterns overlap")
			}
		}
		compiled = append(compiled, pattern)
	}
	return compiled, nil
}

func matchesPackage(packagePath string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == packagePath {
			return true
		}
		if strings.HasSuffix(pattern, "/...") {
			root := strings.TrimSuffix(pattern, "/...")
			if packagePath == root || strings.HasPrefix(packagePath, root+"/") {
				return true
			}
		}
	}
	return false
}

func patternsOverlap(left, right string) bool {
	return matchesPackage(strings.TrimSuffix(left, "/..."), []string{right}) ||
		matchesPackage(strings.TrimSuffix(right, "/..."), []string{left})
}

func exactPackage(packagePath string) bool {
	return packagePath != "" && packagePath != "." && !strings.HasPrefix(packagePath, "/") &&
		path.Clean(packagePath) == packagePath && !strings.Contains(packagePath, "*") &&
		!strings.Contains(packagePath, "...")
}

func validPattern(pattern string) bool {
	if exactPackage(pattern) {
		return true
	}
	return strings.HasSuffix(pattern, "/...") && exactPackage(strings.TrimSuffix(pattern, "/..."))
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

func functionSymbol(function *types.Func) string {
	symbol := function.Name()
	signature := function.Type().(*types.Signature)
	if signature.Recv() != nil {
		receiver := types.Unalias(signature.Recv().Type())
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = types.Unalias(pointer.Elem())
		}
		if named, ok := receiver.(*types.Named); ok {
			symbol = named.Obj().Name() + "." + symbol
		}
	}
	return symbol
}
