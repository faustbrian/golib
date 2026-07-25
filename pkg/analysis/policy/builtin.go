package policy

import (
	"github.com/faustbrian/golib/pkg/analysis/analyzers/backenderror"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/blockingcontext"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/cleanupownership"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/constructorgoroutine"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/forbiddenapi"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/globalgoroutine"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/goroutinefanout"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/httpclienttimeout"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/importboundary"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/interfacenaming"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/interfaceplacement"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/lockacrosscall"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/metriccardinality"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/mutableglobal"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/nobackground"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/nodefaulthttp"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/noinit"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/noprocesscontrol"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/nostoredcontext"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/nounsafe"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/sensitivesink"
	"github.com/faustbrian/golib/pkg/analysis/analyzers/transactionrollback"
)

// Builtin returns the governed inventory shipped by this module.
func Builtin() (*Registry, error) {
	return NewRegistry([]Entry{
		{
			Rule:  backenderror.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/errorlint",
				CanonicalAuthority: "errorlint owns generic wrapping and comparison mechanics; " +
					"analysis owns configured backend provenance at exported boundaries",
				CompatibleConfig: "Keep errorlint enabled for errors.Is, errors.As, and wrapping syntax.",
			}},
		},
		{
			Rule:  lockacrosscall.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{
				{
					Tool: "go vet/copylocks",
					CanonicalAuthority: "go vet owns lock copying; " +
						"analysis owns configured calls under locks",
					CompatibleConfig: "Keep copylocks enabled.",
				},
				{
					Tool: "Staticcheck/SA2001",
					CanonicalAuthority: "Staticcheck owns empty critical sections; " +
						"analysis owns configured calls under locks",
					CompatibleConfig: "Keep SA2001 enabled.",
				},
			},
		},
		{
			Rule:  httpclienttimeout.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{
				{
					Tool: "golangci-lint/noctx",
					CanonicalAuthority: "noctx owns request context; " +
						"analysis owns client timeout construction",
					CompatibleConfig: "Keep noctx enabled for outbound request context.",
				},
				{
					Tool: "gosec/G114",
					CanonicalAuthority: "G114 owns HTTP server timeouts; " +
						"analysis owns client timeouts",
					CompatibleConfig: "Keep G114 enabled for HTTP servers.",
				},
			},
		},
		{
			Rule:  cleanupownership.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{
				{
					Tool: "golangci-lint/bodyclose",
					CanonicalAuthority: "bodyclose owns HTTP response bodies; " +
						"analysis owns configured cleanup results",
					CompatibleConfig: "Keep bodyclose enabled and do not configure " +
						"HTTP response-body contracts here.",
				},
				{
					Tool: "golangci-lint/sqlclosecheck",
					CanonicalAuthority: "sqlclosecheck owns database/sql resources; " +
						"analysis owns configured cleanup results",
					CompatibleConfig: "Keep sqlclosecheck enabled and do not configure " +
						"database/sql contracts here.",
				},
			},
		},
		{
			Rule:  transactionrollback.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/sqlclosecheck",
				CanonicalAuthority: "sqlclosecheck owns Rows, Stmt, NamedStmt, and pgx query closure; " +
					"analysis owns configured transaction rollback establishment",
				CompatibleConfig: "Keep sqlclosecheck enabled for query and statement resources.",
			}},
		},
		{
			Rule:  constructorgoroutine.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  globalgoroutine.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  goroutinefanout.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{
				{
					Tool: "go vet/loopclosure",
					CanonicalAuthority: "go vet owns loop-variable capture lifetime; " +
						"analysis owns configured concurrency fan-out bounds",
					CompatibleConfig: "Keep loopclosure enabled.",
				},
				{
					Tool: "Staticcheck/SA2000",
					CanonicalAuthority: "Staticcheck owns WaitGroup.Add placement; " +
						"analysis owns proven loop concurrency bounds",
					CompatibleConfig: "Keep SA2000 enabled.",
				},
			},
		},
		{
			Rule:  forbiddenapi.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{{
				Tool: "Staticcheck/SA1019",
				CanonicalAuthority: "Staticcheck owns documented deprecations; " +
					"analysis owns organization migrations",
				CompatibleConfig: "Keep SA1019 enabled and configure only organization policy here.",
			}},
		},
		{
			Rule:  interfacenaming.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{{
				Tool: "Staticcheck/ST1003",
				CanonicalAuthority: "ST1003 owns Go initialism spelling; " +
					"analysis owns configured architectural-role affixes",
				CompatibleConfig: "Keep ST1003 enabled and use interface naming policy only for role affixes.",
			}},
		},
		{
			Rule:  interfaceplacement.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{
				{
					Tool: "golangci-lint/ireturn",
					CanonicalAuthority: "ireturn owns interface return policy; " +
						"analysis owns configured declaration placement",
					CompatibleConfig: "Keep ireturn enabled for return-site policy.",
				},
				{
					Tool: "golangci-lint/interfacebloat",
					CanonicalAuthority: "interfacebloat owns method-count thresholds; " +
						"analysis owns configured declaration placement",
					CompatibleConfig: "Keep interfacebloat enabled for interface breadth.",
				},
			},
		},
		{
			Rule:  blockingcontext.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  importboundary.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{{
				Tool:               "golangci-lint/depguard",
				CanonicalAuthority: "analysis",
				CompatibleConfig:   "Disable overlapping depguard package rules.",
			}},
		},
		{
			Rule:  nobackground.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  nodefaulthttp.Rule,
			Owner: "platform-runtime",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/noctx",
				CanonicalAuthority: "noctx owns contextless calls; " +
					"analysis owns shared globals",
				CompatibleConfig: "Keep noctx enabled for HTTP convenience calls.",
			}},
		},
		{
			Rule:  noinit.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  noprocesscontrol.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  nostoredcontext.Rule,
			Owner: "platform-runtime",
		},
		{
			Rule:  sensitivesink.Rule,
			Owner: "platform-security",
			Overlaps: []Overlap{{
				Tool: "gosec/G101",
				CanonicalAuthority: "gosec owns hardcoded credentials; " +
					"analysis owns typed sink flows",
				CompatibleConfig: "Keep G101 enabled for source credential detection.",
			}},
		},
		{
			Rule:  nounsafe.Rule,
			Owner: "platform-security",
			Overlaps: []Overlap{{
				Tool:               "gosec/G103",
				CanonicalAuthority: "analysis",
				CompatibleConfig:   "Keep G103 advisory for unsafe call detail.",
			}},
		},
		{
			Rule:  mutableglobal.Rule,
			Owner: "platform-architecture",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/gochecknoglobals",
				CanonicalAuthority: "analysis owns typed shared mutable state; " +
					"gochecknoglobals owns blanket global-style prohibition",
				CompatibleConfig: "Disable gochecknoglobals for packages governed by this rule.",
			}},
		},
		{
			Rule:  metriccardinality.Rule,
			Owner: "platform-observability",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/promlinter",
				CanonicalAuthority: "promlinter owns metric naming and help conventions; " +
					"analysis owns configured typed cardinality flows",
				CompatibleConfig: "Keep promlinter enabled for Prometheus conventions.",
			}},
		},
		{
			Rule:  metriccardinality.LabelNameRule,
			Owner: "platform-observability",
			Overlaps: []Overlap{{
				Tool: "golangci-lint/promlinter",
				CanonicalAuthority: "promlinter owns metric naming and help conventions; " +
					"analysis owns typed attacker-controlled label-name flows",
				CompatibleConfig: "Keep promlinter enabled for Prometheus conventions.",
			}},
		},
	})
}
