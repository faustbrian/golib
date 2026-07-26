// Package nounsafe forbids unsafe, cgo, and go:linkname in production policy.
package nounsafe

import (
	"go/ast"
	"strconv"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "security/no-unsafe"

// Rule is the stable metadata for the unsafe boundary policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategorySecurity,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Unsafe, cgo, and linkname bypass Go safety boundaries.",
	Remediation:       "Use a safe Go API or isolate the operation in an approved package.",
	IntroducedVersion: "0.1.0",
}

// Analyzer reports unsafe boundary bypasses at their exact syntax location.
var Analyzer = &analysis.Analyzer{
	Name: "nounsafe",
	Doc:  Rule.Rationale,
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			switch path {
			case "unsafe":
				pass.Reportf(imported.Path.Pos(), "%s: production package imports unsafe", ruleID)
			case "C":
				pass.Reportf(imported.Path.Pos(), "%s: production package imports C", ruleID)
			}
		}
		reportLinkname(pass, file)
	}

	return nil, nil
}

func reportLinkname(pass *analysis.Pass, file *ast.File) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			fields := strings.Fields(comment.Text)
			if fields[0] == "//go:linkname" {
				pass.Reportf(comment.Pos(), "%s: production code uses go:linkname", ruleID)
			}
		}
	}
}
