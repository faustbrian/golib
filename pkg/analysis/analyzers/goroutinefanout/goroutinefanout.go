// Package goroutinefanout detects goroutine launches repeated by loops without
// a statically proven organization-approved bound.
package goroutinefanout

import (
	"errors"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const (
	ruleID              = "lifecycle/unbounded-goroutine-fanout"
	maximumStaticFanout = 1024
)

// Policy opts one package or trailing /... tree into fan-out enforcement.
type Policy struct {
	Package   string
	MaxStatic int
}

// Options configures package fan-out limits.
type Options struct {
	Policies []Policy
}

type compiledPolicy struct {
	pattern   string
	maxStatic int64
}

type loopFrame struct {
	statement ast.Stmt
}

type valueKey struct {
	root  types.Object
	field *types.Var
}

type walker struct {
	pass              *analysis.Pass
	maxStatic         int64
	channelCapacities map[valueKey]int64
}

// Rule is the stable metadata for goroutine fan-out policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityWarning,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Loop-driven goroutine launches can make concurrency scale with untrusted or unbounded work.",
	Remediation:       "Use a fixed worker pool, a proven bounded semaphore, or synchronous per-iteration completion.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"goroutine_fanout": {
			Type:        shared.ConfigurationArray,
			Description: "Package trees and maximum statically expanded goroutine launches.",
		},
	}},
}

// Analyzer is inactive until package policies are configured.
var Analyzer, _ = New(Options{})

// New validates fan-out policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	policies := make([]compiledPolicy, 0, len(options.Policies))
	for _, policy := range options.Policies {
		if !validPattern(policy.Package) || policy.MaxStatic < 1 ||
			policy.MaxStatic > maximumStaticFanout {
			return nil, errors.New(
				"goroutine fan-out policy requires a package pattern and max_static from 1 through 1024",
			)
		}
		for _, existing := range policies {
			if patternsOverlap(existing.pattern, policy.Package) {
				return nil, errors.New("goroutine fan-out package policies overlap")
			}
		}
		policies = append(policies, compiledPolicy{
			pattern: policy.Package, maxStatic: int64(policy.MaxStatic),
		})
	}

	return &analysis.Analyzer{
		Name: "goroutinefanout",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			maxStatic, configured := packageLimit(pass.Pkg.Path(), policies)
			if !configured {
				return nil, nil
			}
			current := walker{
				pass: pass, maxStatic: maxStatic,
				channelCapacities: collectChannelCapacities(pass),
			}
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if ok && function.Body != nil {
						current.block(function.Body, nil)
					}
				}
			}
			return nil, nil
		},
	}, nil
}

func (current walker) block(block *ast.BlockStmt, loops []loopFrame) {
	current.statements(block.List, loops)
}

func (current walker) statements(statements []ast.Stmt, loops []loopFrame) {
	for index, statement := range statements {
		switch statement := statement.(type) {
		case *ast.GoStmt:
			current.reportGo(statement, statements, index, loops)
		case *ast.ForStmt:
			current.block(statement.Body, appendLoop(loops, statement))
		case *ast.RangeStmt:
			current.block(statement.Body, appendLoop(loops, statement))
		case *ast.BlockStmt:
			current.block(statement, loops)
		case *ast.IfStmt:
			current.block(statement.Body, loops)
			if statement.Else != nil {
				current.statement(statement.Else, loops)
			}
		case *ast.SwitchStmt:
			current.clauses(statement.Body.List, loops)
		case *ast.TypeSwitchStmt:
			current.clauses(statement.Body.List, loops)
		case *ast.SelectStmt:
			current.clauses(statement.Body.List, loops)
		case *ast.LabeledStmt:
			current.statement(statement.Stmt, loops)
		case *ast.ExprStmt:
			current.immediateCall(statement.X, loops)
		case *ast.DeferStmt:
			current.immediateFunction(statement.Call, loops)
		}
	}
}

func (current walker) statement(statement ast.Stmt, loops []loopFrame) {
	current.statements([]ast.Stmt{statement}, loops)
}

func (current walker) clauses(clauses []ast.Stmt, loops []loopFrame) {
	for _, clause := range clauses {
		switch clause := clause.(type) {
		case *ast.CaseClause:
			current.statements(clause.Body, loops)
		case *ast.CommClause:
			current.statements(clause.Body, loops)
		}
	}
}

func (current walker) immediateCall(expression ast.Expr, loops []loopFrame) {
	call, ok := expression.(*ast.CallExpr)
	if ok {
		current.immediateFunction(call, loops)
	}
}

func (current walker) immediateFunction(call *ast.CallExpr, loops []loopFrame) {
	function, ok := unparen(call.Fun).(*ast.FuncLit)
	if ok {
		current.block(function.Body, loops)
	}
}

func (current walker) reportGo(
	statement *ast.GoStmt,
	statements []ast.Stmt,
	index int,
	loops []loopFrame,
) {
	if len(loops) == 0 || current.synchronized(statement, statements, index) ||
		current.loopsWithinLimit(loops) {
		return
	}
	current.pass.Reportf(
		statement.Go,
		"%s: goroutine launch is repeated without a proven limit of %d",
		ruleID,
		current.maxStatic,
	)
}

func (current walker) loopsWithinLimit(loops []loopFrame) bool {
	product := int64(1)
	for _, loop := range loops {
		iterations, known := current.staticIterations(loop.statement)
		if !known {
			return false
		}
		if iterations == 0 {
			return true
		}
		if iterations > current.maxStatic {
			return false
		}
		product *= iterations
		if product > current.maxStatic {
			return false
		}
	}
	return true
}

func (current walker) staticIterations(statement ast.Stmt) (int64, bool) {
	switch statement := statement.(type) {
	case *ast.RangeStmt:
		return current.rangeIterations(statement.X)
	case *ast.ForStmt:
		return current.forIterations(statement)
	default:
		return 0, false
	}
}

func (current walker) rangeIterations(expression ast.Expr) (int64, bool) {
	if value := current.pass.TypesInfo.Types[expression].Value; value != nil {
		switch value.Kind() {
		case constant.Int:
			count, exact := constant.Int64Val(value)
			return count, exact && count >= 0
		case constant.String:
			return int64(len(constant.StringVal(value))), true
		}
	}
	valueType := types.Unalias(current.pass.TypesInfo.TypeOf(expression))
	if pointer, ok := valueType.(*types.Pointer); ok {
		valueType = types.Unalias(pointer.Elem())
	}
	if array, ok := valueType.(*types.Array); ok {
		return array.Len(), true
	}
	if literal, ok := unparen(expression).(*ast.CompositeLit); ok {
		switch types.Unalias(current.pass.TypesInfo.TypeOf(literal)).Underlying().(type) {
		case *types.Slice, *types.Map:
			return int64(len(literal.Elts)), true
		}
	}
	return 0, false
}

func (current walker) forIterations(loop *ast.ForStmt) (int64, bool) {
	variable, start, ok := current.loopStart(loop.Init)
	if !ok {
		return 0, false
	}
	condition, ok := loop.Cond.(*ast.BinaryExpr)
	if !ok || objectOf(current.pass, condition.X) != variable {
		return 0, false
	}
	limit, ok := integerConstant(current.pass, condition.Y)
	if !ok {
		return 0, false
	}
	post, ok := loop.Post.(*ast.IncDecStmt)
	if !ok || objectOf(current.pass, post.X) != variable {
		return 0, false
	}
	return countIterations(start, limit, condition.Op, post.Tok)
}

func (current walker) loopStart(statement ast.Stmt) (types.Object, int64, bool) {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, 0, false
	}
	identifier, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, 0, false
	}
	start, ok := integerConstant(current.pass, assignment.Rhs[0])
	return objectOf(current.pass, identifier), start, ok
}

func countIterations(start, limit int64, comparison, post token.Token) (int64, bool) {
	var count int64
	current := start
	for compare(current, limit, comparison) {
		count++
		if count > maximumStaticFanout {
			return count, true
		}
		switch post {
		case token.INC:
			if current == math.MaxInt64 {
				return 0, false
			}
			current++
		case token.DEC:
			if current == math.MinInt64 {
				return 0, false
			}
			current--
		default:
			return 0, false
		}
	}
	return count, true
}

func compare(left, right int64, operation token.Token) bool {
	switch operation {
	case token.LSS:
		return left < right
	case token.LEQ:
		return left <= right
	case token.GTR:
		return left > right
	case token.GEQ:
		return left >= right
	default:
		return false
	}
}

func (current walker) synchronized(
	statement *ast.GoStmt,
	statements []ast.Stmt,
	index int,
) bool {
	function, ok := unparen(statement.Call.Fun).(*ast.FuncLit)
	if !ok {
		return false
	}
	return current.channelSynchronized(function, statements[:index]) ||
		current.waitGroupSynchronized(function, statements, statement)
}

func (current walker) channelSynchronized(
	function *ast.FuncLit,
	before []ast.Stmt,
) bool {
	for _, candidate := range before {
		send, ok := candidate.(*ast.SendStmt)
		if !ok {
			continue
		}
		channel, identified := valueIdentity(current.pass, send.Chan)
		if !identified {
			continue
		}
		capacity, known := current.channelCapacities[channel]
		if known && capacity <= current.maxStatic && deferredReceive(current.pass, function, channel) {
			return true
		}
	}
	return false
}

func (current walker) waitGroupSynchronized(
	function *ast.FuncLit,
	statements []ast.Stmt,
	launch ast.Stmt,
) bool {
	groups := make(map[valueKey]struct{})
	for _, candidate := range statements {
		if candidate == launch {
			break
		}
		call := expressionCall(candidate)
		group, ok := waitGroupReceiver(current.pass, call, "Add")
		increment, positive := waitGroupIncrement(current.pass, call)
		if ok && positive && increment > 0 {
			groups[group] = struct{}{}
		}
	}
	for group := range groups {
		if !containsWaitGroupCall(current.pass, function.Body, group, "Done") {
			continue
		}
		afterLaunch := false
		for _, candidate := range statements {
			if !afterLaunch {
				afterLaunch = candidate == launch
				continue
			}
			call := expressionCall(candidate)
			waited, ok := waitGroupReceiver(current.pass, call, "Wait")
			if ok && waited == group {
				return true
			}
		}
	}
	return false
}

func collectChannelCapacities(pass *analysis.Pass) map[valueKey]int64 {
	capacities := make(map[valueKey]int64)
	seen := make(map[valueKey]struct{})
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				if len(node.Rhs) == len(node.Lhs) {
					for index, right := range node.Rhs {
						recordChannelCapacity(pass, capacities, seen, node.Lhs[index], right)
					}
				}
			case *ast.ValueSpec:
				if len(node.Values) == len(node.Names) {
					for index, value := range node.Values {
						recordChannelCapacity(pass, capacities, seen, node.Names[index], value)
					}
				}
			}
			return true
		})
	}
	return capacities
}

func recordChannelCapacity(
	pass *analysis.Pass,
	capacities map[valueKey]int64,
	seen map[valueKey]struct{},
	left ast.Expr,
	right ast.Expr,
) {
	identity, identified := valueIdentity(pass, left)
	if !identified || !isChannel(pass.TypesInfo.TypeOf(left)) {
		return
	}
	if _, duplicate := seen[identity]; duplicate {
		delete(capacities, identity)
		return
	}
	seen[identity] = struct{}{}
	call, ok := unparen(right).(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || !isBuiltin(pass, call.Fun, "make") {
		return
	}
	capacity, ok := integerConstant(pass, call.Args[1])
	if ok && capacity > 0 {
		capacities[identity] = capacity
	}
}

func isChannel(value types.Type) bool {
	if value == nil {
		return false
	}
	_, ok := types.Unalias(value).Underlying().(*types.Chan)
	return ok
}

func deferredReceive(pass *analysis.Pass, function *ast.FuncLit, channel valueKey) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		deferred, ok := node.(*ast.DeferStmt)
		if !ok {
			return true
		}
		function, ok := unparen(deferred.Call.Fun).(*ast.FuncLit)
		if !ok {
			return true
		}
		inspectExecutedBody(function.Body, func(node ast.Node) bool {
			receive, ok := node.(*ast.UnaryExpr)
			if ok && receive.Op == token.ARROW {
				identity, identified := valueIdentity(pass, receive.X)
				found = identified && identity == channel
			}
			return !found
		})
		return !found
	})
	return found
}

func containsWaitGroupCall(
	pass *analysis.Pass,
	node ast.Node,
	receiver valueKey,
	method string,
) bool {
	found := false
	inspectExecutedBody(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			calledReceiver, matches := waitGroupReceiver(pass, call, method)
			found = matches && calledReceiver == receiver
		}
		return !found
	})
	return found
}

func inspectExecutedBody(node ast.Node, visit func(ast.Node) bool) {
	ast.Inspect(node, func(current ast.Node) bool {
		if current != node {
			if _, nested := current.(*ast.FuncLit); nested {
				return false
			}
		}
		return visit(current)
	})
}

func waitGroupIncrement(pass *analysis.Pass, call *ast.CallExpr) (int64, bool) {
	if call == nil || len(call.Args) != 1 {
		return 0, false
	}
	return integerConstant(pass, call.Args[0])
}

func waitGroupReceiver(
	pass *analysis.Pass,
	call *ast.CallExpr,
	method string,
) (valueKey, bool) {
	if call == nil {
		return valueKey{}, false
	}
	selector, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return valueKey{}, false
	}
	function, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
	if !ok || !isWaitGroupMethod(function, method) {
		return valueKey{}, false
	}
	return valueIdentity(pass, selector.X)
}

func isWaitGroupMethod(function *types.Func, method string) bool {
	if function.Pkg() == nil || function.Pkg().Path() != "sync" ||
		function.Name() != method {
		return false
	}
	receiver := function.Type().(*types.Signature).Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := receiver.(*types.Named)
	if !ok || named.Obj().Name() != "WaitGroup" {
		return false
	}
	return true
}

func expressionCall(statement ast.Stmt) *ast.CallExpr {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, _ := unparen(expression.X).(*ast.CallExpr)
	return call
}

func isBuiltin(pass *analysis.Pass, expression ast.Expr, name string) bool {
	identifier, ok := unparen(expression).(*ast.Ident)
	if !ok {
		return false
	}
	builtin, ok := pass.TypesInfo.Uses[identifier].(*types.Builtin)
	return ok && builtin.Name() == name
}

func objectOf(pass *analysis.Pass, expression ast.Expr) types.Object {
	identifier, ok := unparen(expression).(*ast.Ident)
	if !ok {
		return nil
	}
	if object := pass.TypesInfo.Defs[identifier]; object != nil {
		return object
	}
	return pass.TypesInfo.Uses[identifier]
}

func valueIdentity(pass *analysis.Pass, expression ast.Expr) (valueKey, bool) {
	switch expression := unparen(expression).(type) {
	case *ast.Ident:
		object := pass.TypesInfo.Uses[expression]
		if object == nil {
			object = pass.TypesInfo.Defs[expression]
		}
		if _, ok := object.(*types.Var); !ok {
			return valueKey{}, false
		}
		return valueKey{root: object}, true
	case *ast.StarExpr:
		return valueIdentity(pass, expression.X)
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return valueIdentity(pass, expression.X)
		}
		return valueKey{}, false
	case *ast.SelectorExpr:
		selection := pass.TypesInfo.Selections[expression]
		if selection == nil {
			return valueKey{}, false
		}
		field, ok := selection.Obj().(*types.Var)
		if !ok {
			return valueKey{}, false
		}
		root, ok := valueIdentity(pass, expression.X)
		if !ok || root.field != nil {
			return valueKey{}, false
		}
		root.field = field
		return root, true
	default:
		return valueKey{}, false
	}
}

func integerConstant(pass *analysis.Pass, expression ast.Expr) (int64, bool) {
	value := pass.TypesInfo.Types[expression].Value
	if value == nil || value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(value)
}

func unparen(expression ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = parenthesized.X
	}
}

func appendLoop(loops []loopFrame, statement ast.Stmt) []loopFrame {
	appended := make([]loopFrame, len(loops), len(loops)+1)
	copy(appended, loops)
	return append(appended, loopFrame{statement: statement})
}

func packageLimit(packagePath string, policies []compiledPolicy) (int64, bool) {
	for _, policy := range policies {
		if matchesPackage(packagePath, policy.pattern) {
			return policy.maxStatic, true
		}
	}
	return 0, false
}

func matchesPackage(packagePath, pattern string) bool {
	if packagePath == pattern {
		return true
	}
	if strings.HasSuffix(pattern, "/...") {
		root := strings.TrimSuffix(pattern, "/...")
		return packagePath == root || strings.HasPrefix(packagePath, root+"/")
	}
	return false
}

func patternsOverlap(left, right string) bool {
	return matchesPackage(strings.TrimSuffix(left, "/..."), right) ||
		matchesPackage(strings.TrimSuffix(right, "/..."), left)
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
