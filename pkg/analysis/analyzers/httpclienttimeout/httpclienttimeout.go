// Package httpclienttimeout requires explicit timeout ownership for HTTP clients.
package httpclienttimeout

import (
	"errors"
	"go/ast"
	"go/constant"
	"go/types"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "http/client-timeout"

// Options configures reviewed packages that intentionally use unbounded clients.
type Options struct {
	AllowedPackages []string
}

// Rule is the stable metadata for explicit HTTP client timeout policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryHTTP,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "HTTP clients without explicit positive timeouts can retain blocked operations and resources indefinitely.",
	Remediation:       "Set Client.Timeout to a positive reviewed duration, or isolate an intentional streaming client in an approved package.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"http_timeout_exceptions": {
			Type:        shared.ConfigurationArray,
			Description: "Exact packages allowed to construct clients without a positive timeout.",
		},
	}},
}

// Analyzer applies timeout policy with no package exceptions.
var Analyzer, _ = New(Options{})

// New validates timeout exceptions and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	allowed := make(map[string]struct{}, len(options.AllowedPackages))
	for _, packagePath := range options.AllowedPackages {
		if !exactPackage(packagePath) {
			return nil, errors.New("HTTP timeout exceptions require exact package paths")
		}
		allowed[packagePath] = struct{}{}
	}

	return &analysis.Analyzer{
		Name: "httpclienttimeout",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if _, exempt := allowed[pass.Pkg.Path()]; exempt {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.CompositeLit:
						reportComposite(pass, node)
					case *ast.CallExpr:
						reportNew(pass, node)
					case *ast.ValueSpec:
						reportZeroValue(pass, node)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
}

func reportComposite(pass *analysis.Pass, literal *ast.CompositeLit) {
	if !isHTTPClient(pass.TypesInfo.TypeOf(literal)) {
		return
	}
	var timeout ast.Expr
	for _, element := range literal.Elts {
		keyed, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return
		}
		identifier, ok := keyed.Key.(*ast.Ident)
		if ok && identifier.Name == "Timeout" {
			timeout = keyed.Value
		}
	}
	if timeout == nil {
		pass.Reportf(
			literal.Type.Pos(),
			"%s: http.Client must declare a positive Timeout",
			ruleID,
		)
		return
	}
	value := pass.TypesInfo.Types[timeout].Value
	if value != nil && constant.Sign(value) <= 0 {
		pass.Reportf(
			timeout.Pos(),
			"%s: http.Client Timeout must be positive",
			ruleID,
		)
	}
}

func reportNew(pass *analysis.Pass, call *ast.CallExpr) {
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 1 {
		return
	}
	builtin, ok := pass.TypesInfo.Uses[identifier].(*types.Builtin)
	if !ok || builtin.Name() != "new" || !isHTTPClient(pass.TypesInfo.TypeOf(call.Args[0])) {
		return
	}
	pass.Reportf(
		call.Args[0].Pos(),
		"%s: new(http.Client) has no explicit timeout policy",
		ruleID,
	)
}

func reportZeroValue(pass *analysis.Pass, specification *ast.ValueSpec) {
	if specification.Type == nil || len(specification.Values) != 0 ||
		!isHTTPClient(pass.TypesInfo.TypeOf(specification.Type)) {
		return
	}
	for _, name := range specification.Names {
		pass.Reportf(
			name.Pos(),
			"%s: zero-value http.Client has no explicit timeout policy",
			ruleID,
		)
	}
}

func isHTTPClient(value types.Type) bool {
	value = types.Unalias(value)
	named, ok := value.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Client"
}

func exactPackage(packagePath string) bool {
	return packagePath != "" && path.Clean(packagePath) == packagePath &&
		!strings.Contains(packagePath, "*")
}
