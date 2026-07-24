// Package lockacrosscall detects configured calls made while locks are held.
package lockacrosscall

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/cfg"
)

const (
	ruleID                = "lifecycle/lock-across-call"
	maximumCFGBlocks      = 4096
	maximumLockIdentities = 256
)

// Call identifies one exact callback or I/O callable.
type Call struct {
	Package         string
	Symbol          string
	AllowedPackages []string
}

// Options configures calls that must not run while a lock is held.
type Options struct {
	Calls []Call
}

type symbolKey struct {
	packagePath string
	symbol      string
}

type compiledCall struct {
	allowed map[string]struct{}
}

type lockKey struct {
	root  types.Object
	field *types.Var
}

type lockSet map[lockKey]struct{}

type lockAction uint8

const (
	lockNone lockAction = iota
	lockAcquire
	lockRelease
)

// Rule is the stable metadata for calls made under internal locks.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Callbacks and blocking I/O under an internal lock can deadlock, amplify contention, and transfer lock-order control.",
	Remediation:       "Copy required state, release the lock, then invoke the configured callback or I/O operation.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"lock_sensitive_calls": {
			Type:        shared.ConfigurationArray,
			Description: "Exact callback or I/O callables forbidden while a lock is definitely held.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates lock-sensitive call policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	calls := make(map[symbolKey]compiledCall, len(options.Calls))
	for _, call := range options.Calls {
		if !exactPackage(call.Package) || !validSymbol(call.Symbol) {
			return nil, errors.New("lock-sensitive calls require an exact package and Function or Type.Method symbol")
		}
		key := symbolKey{packagePath: call.Package, symbol: call.Symbol}
		if _, duplicate := calls[key]; duplicate {
			return nil, errors.New("lock-sensitive call policy contains a duplicate callable")
		}
		compiled := compiledCall{allowed: make(map[string]struct{}, len(call.AllowedPackages))}
		for _, packagePath := range call.AllowedPackages {
			if !exactPackage(packagePath) {
				return nil, errors.New("lock-sensitive call exceptions require exact package paths")
			}
			compiled.allowed[packagePath] = struct{}{}
		}
		calls[key] = compiled
	}

	return &analysis.Analyzer{
		Name: "lockacrosscall",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(calls) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				var analysisErr error
				ast.Inspect(file, func(node ast.Node) bool {
					if analysisErr != nil {
						return false
					}
					switch node := node.(type) {
					case *ast.FuncDecl:
						if node.Body != nil {
							analysisErr = analyzeBody(pass, node.Body, calls)
						}
					case *ast.FuncLit:
						analysisErr = analyzeBody(pass, node.Body, calls)
					}
					return analysisErr == nil
				})
				if analysisErr != nil {
					return nil, analysisErr
				}
			}
			return nil, nil
		},
	}, nil
}

func analyzeBody(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	calls map[symbolKey]compiledCall,
) error {
	universe := lockUniverse(pass, body)
	if len(universe) > maximumLockIdentities {
		return fmt.Errorf(
			"lock analysis exceeds %d lock identities",
			maximumLockIdentities,
		)
	}
	if len(universe) == 0 {
		return nil
	}
	graph := cfg.New(body, func(*ast.CallExpr) bool { return true })
	if err := validateCFGBlockCount(len(graph.Blocks)); err != nil {
		return err
	}
	inputs := make([]lockSet, len(graph.Blocks))
	outputs := make([]lockSet, len(graph.Blocks))
	predecessors := make([][]*cfg.Block, len(graph.Blocks))
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		inputs[block.Index] = cloneLocks(universe)
		outputs[block.Index] = cloneLocks(universe)
		for _, successor := range block.Succs {
			predecessors[successor.Index] = append(predecessors[successor.Index], block)
		}
	}
	inputs[0] = make(lockSet)

	changed := true
	for changed {
		changed = false
		for _, block := range graph.Blocks {
			if !block.Live {
				continue
			}
			var input lockSet
			if block.Index == 0 {
				input = make(lockSet)
			} else {
				input = intersectPredecessors(predecessors[block.Index], outputs, universe)
			}
			output := transferBlock(pass, block, input, calls, false)
			if !maps.Equal(inputs[block.Index], input) ||
				!maps.Equal(outputs[block.Index], output) {
				inputs[block.Index] = input
				outputs[block.Index] = output
				changed = true
			}
		}
	}
	for _, block := range graph.Blocks {
		if block.Live {
			transferBlock(pass, block, inputs[block.Index], calls, true)
		}
	}
	return nil
}

func validateCFGBlockCount(blocks int) error {
	if blocks > maximumCFGBlocks {
		return fmt.Errorf("lock analysis exceeds %d CFG blocks", maximumCFGBlocks)
	}
	return nil
}

func transferBlock(
	pass *analysis.Pass,
	block *cfg.Block,
	input lockSet,
	calls map[symbolKey]compiledCall,
	report bool,
) lockSet {
	held := cloneLocks(input)
	for _, node := range block.Nodes {
		key, action, deferred := lockOperation(pass, node)
		if action != lockNone {
			if deferred {
				continue
			}
			if action == lockAcquire {
				held[key] = struct{}{}
			} else {
				delete(held, key)
			}
			continue
		}
		if report && len(held) > 0 {
			for _, called := range configuredCalls(pass, node, calls) {
				pass.Reportf(
					called.position,
					"%s: %s.%s is called while a lock is definitely held",
					ruleID,
					called.function.Pkg().Path(),
					functionSymbol(called.function),
				)
			}
		}
	}
	return held
}

type resolvedCall struct {
	function *types.Func
	position token.Pos
}

func configuredCalls(
	pass *analysis.Pass,
	node ast.Node,
	calls map[symbolKey]compiledCall,
) []resolvedCall {
	switch node.(type) {
	case *ast.DeferStmt, *ast.GoStmt:
		return nil
	}
	var resolved []resolvedCall
	ast.Inspect(node, func(nested ast.Node) bool {
		if _, literal := nested.(*ast.FuncLit); literal {
			return false
		}
		call, ok := nested.(*ast.CallExpr)
		if !ok {
			return true
		}
		function, ok := calledObject(pass, call.Fun).(*types.Func)
		if !ok || function.Pkg() == nil {
			return true
		}
		configured, ok := calls[symbolKey{
			packagePath: function.Pkg().Path(),
			symbol:      functionSymbol(function),
		}]
		if !ok {
			return true
		}
		if _, allowed := configured.allowed[pass.Pkg.Path()]; allowed {
			return true
		}
		resolved = append(resolved, resolvedCall{function: function, position: call.Pos()})
		return true
	})
	return resolved
}

func lockUniverse(pass *analysis.Pass, body *ast.BlockStmt) lockSet {
	locks := make(lockSet)
	ast.Inspect(body, func(node ast.Node) bool {
		if len(locks) > maximumLockIdentities {
			return false
		}
		key, action, _ := lockOperation(pass, node)
		if action != lockNone {
			locks[key] = struct{}{}
		}
		return true
	})
	return locks
}

func lockOperation(pass *analysis.Pass, node ast.Node) (lockKey, lockAction, bool) {
	var call *ast.CallExpr
	deferred := false
	switch node := node.(type) {
	case *ast.ExprStmt:
		call, _ = node.X.(*ast.CallExpr)
	case *ast.DeferStmt:
		call = node.Call
		deferred = true
	default:
		return lockKey{}, lockNone, false
	}
	if call == nil {
		return lockKey{}, lockNone, deferred
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return lockKey{}, lockNone, deferred
	}
	function, ok := calledObject(pass, call.Fun).(*types.Func)
	if !ok || !syncLockMethod(function) {
		return lockKey{}, lockNone, deferred
	}
	key, ok := lockIdentity(pass, selector.X)
	if !ok {
		return lockKey{}, lockNone, deferred
	}
	if function.Name() == "Lock" || function.Name() == "RLock" {
		return key, lockAcquire, deferred
	}
	return key, lockRelease, deferred
}

func syncLockMethod(function *types.Func) bool {
	if function.Pkg() == nil || function.Pkg().Path() != "sync" {
		return false
	}
	switch function.Name() {
	case "Lock", "Unlock", "RLock", "RUnlock":
	default:
		return false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := receiver.(*types.Named)
	return ok && (named.Obj().Name() == "Mutex" || named.Obj().Name() == "RWMutex")
}

func lockIdentity(pass *analysis.Pass, expression ast.Expr) (lockKey, bool) {
	switch expression := expression.(type) {
	case *ast.ParenExpr:
		return lockIdentity(pass, expression.X)
	case *ast.StarExpr:
		return lockIdentity(pass, expression.X)
	case *ast.Ident:
		object := pass.TypesInfo.Uses[expression]
		if object == nil {
			object = pass.TypesInfo.Defs[expression]
		}
		if _, ok := object.(*types.Var); !ok {
			return lockKey{}, false
		}
		return lockKey{root: object}, true
	case *ast.SelectorExpr:
		selection := pass.TypesInfo.Selections[expression]
		if selection == nil {
			return lockKey{}, false
		}
		field, ok := selection.Obj().(*types.Var)
		if !ok {
			return lockKey{}, false
		}
		root, ok := lockIdentity(pass, expression.X)
		if !ok || root.field != nil {
			return lockKey{}, false
		}
		root.field = field
		return root, true
	default:
		return lockKey{}, false
	}
}

func intersectPredecessors(
	predecessors []*cfg.Block,
	outputs []lockSet,
	universe lockSet,
) lockSet {
	intersection := cloneLocks(universe)
	if len(predecessors) == 0 {
		return make(lockSet)
	}
	for _, predecessor := range predecessors {
		for key := range intersection {
			if _, held := outputs[predecessor.Index][key]; !held {
				delete(intersection, key)
			}
		}
	}
	return intersection
}

func cloneLocks(source lockSet) lockSet {
	cloned := make(lockSet, len(source))
	for key := range source {
		cloned[key] = struct{}{}
	}
	return cloned
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
	if signature.Recv() == nil {
		return symbol
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := receiver.(*types.Named); ok {
		return named.Obj().Name() + "." + symbol
	}
	return symbol
}
