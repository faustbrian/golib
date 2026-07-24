package analysistestkit

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

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
	"golang.org/x/tools/go/analysis"
)

const representativeSource = `package fuzzpkg
import (
    "context"
    "fmt"
    "net/http"
	"sync"
    "unsafe"
)
type Secret string
type Service struct { ctx context.Context }
type Client interface { Call() }
type Tx struct{}
func (*Tx) Rollback() error { return nil }
var mutex sync.Mutex
var started = func() int { go func() {}(); return 1 }()
func init() {}
func Fetch() {}
func New() { go func() {}() }
func Backend() error { return nil }
func Boundary() error { return Backend() }
func Fanout(values []int) { for range values { go func() {}() } }
func Open() (int, func(), error) { return 0, func() {}, nil }
func Begin() (*Tx, error) { return &Tx{}, nil }
func BlockingIO() {}
func Use(secret Secret) {
	_, _, _ = Open()
	tx, _ := Begin()
	_ = tx
	mutex.Lock()
	BlockingIO()
	mutex.Unlock()
    _ = &http.Client{}
    _ = context.Background()
    _ = http.DefaultClient
    fmt.Printf("%v", secret)
    _ = unsafe.Pointer(nil)
    panic("stop")
}
`

func configuredAnalyzers(tb testing.TB) []*analysis.Analyzer {
	tb.Helper()

	blocking, err := blockingcontext.New(blockingcontext.Options{
		Policies: []blockingcontext.Policy{{
			Package: "fuzzpkg", Functions: []string{"Fetch"},
		}},
	})
	if err != nil {
		tb.Fatalf("blockingcontext.New() error = %v", err)
	}
	constructors, err := constructorgoroutine.New(constructorgoroutine.Options{
		Policies: []constructorgoroutine.Policy{{
			Package: "fuzzpkg", Symbols: []string{"New"},
		}},
	})
	if err != nil {
		tb.Fatalf("constructorgoroutine.New() error = %v", err)
	}
	cleanup, err := cleanupownership.New(cleanupownership.Options{
		Constructors: []cleanupownership.Constructor{{
			Package: "fuzzpkg", Symbol: "Open", CleanupResult: 1,
		}},
	})
	if err != nil {
		tb.Fatalf("cleanupownership.New() error = %v", err)
	}
	transactions, err := transactionrollback.New(transactionrollback.Options{
		Transactions: []transactionrollback.Transaction{{
			Package: "fuzzpkg", Symbol: "Begin", Result: 0,
			RollbackMethod: "Rollback",
		}},
	})
	if err != nil {
		tb.Fatalf("transactionrollback.New() error = %v", err)
	}
	forbidden, err := forbiddenapi.New(forbiddenapi.Options{
		Policies: []forbiddenapi.Policy{{
			Package: "fmt", Symbol: "Println", Replacement: "log/slog.Info",
		}},
	})
	if err != nil {
		tb.Fatalf("forbiddenapi.New() error = %v", err)
	}
	boundaries, err := importboundary.New(importboundary.Options{
		RestrictedDependencies: []importboundary.RestrictedDependency{{
			Package: "net/http", AllowedPackages: []string{"fuzzpkg/adapters/..."},
		}},
		Policies: []importboundary.Policy{{
			Package: "fuzzpkg", DenyImports: []string{"unsafe"},
		}},
		Packages: []importboundary.PackageClass{
			{Package: "fuzzpkg", Layer: "application", Context: "orders"},
			{Package: "context", Layer: "shared", Context: "shared"},
			{Package: "fmt", Layer: "domain", Context: "catalog"},
			{Package: "net/http", Layer: "infrastructure", Context: "orders"},
			{Package: "sync", Layer: "shared", Context: "shared"},
			{Package: "unsafe", Layer: "infrastructure", Context: "orders"},
		},
		Layers: []importboundary.Direction{
			{Name: "application", MayImport: []string{"domain", "shared"}},
			{Name: "domain", MayImport: []string{"shared"}},
			{Name: "infrastructure", MayImport: []string{"application", "domain", "shared"}},
			{Name: "shared"},
		},
		Contexts: []importboundary.Direction{
			{Name: "orders", MayImport: []string{"shared"}},
			{Name: "catalog", MayImport: []string{"shared"}},
			{Name: "shared"},
		},
	})
	if err != nil {
		tb.Fatalf("importboundary.New() error = %v", err)
	}
	lockedCalls, err := lockacrosscall.New(lockacrosscall.Options{
		Calls: []lockacrosscall.Call{{Package: "fuzzpkg", Symbol: "BlockingIO"}},
	})
	if err != nil {
		tb.Fatalf("lockacrosscall.New() error = %v", err)
	}
	variadicFrom := 1
	sensitive, err := sensitivesink.New(sensitivesink.Options{
		SensitiveTypes: []sensitivesink.SensitiveType{{
			Package: "fuzzpkg", Name: "Secret",
		}},
		Sinks: []sensitivesink.Sink{{
			Package: "fmt", Symbol: "Printf", VariadicFrom: &variadicFrom,
		}},
	})
	if err != nil {
		tb.Fatalf("sensitivesink.New() error = %v", err)
	}
	mutableGlobals, err := mutableglobal.New(mutableglobal.Options{
		Policies: []mutableglobal.Policy{{Package: "fuzzpkg"}},
	})
	if err != nil {
		tb.Fatalf("mutableglobal.New() error = %v", err)
	}
	interfaces, err := interfaceplacement.New(interfaceplacement.Options{
		Packages: []string{"fuzzpkg"},
	})
	if err != nil {
		tb.Fatalf("interfaceplacement.New() error = %v", err)
	}
	interfaceNames, err := interfacenaming.New(interfacenaming.Options{
		Policies: []interfacenaming.Policy{{Package: "fuzzpkg", RequiredSuffix: "Port"}},
	})
	if err != nil {
		tb.Fatalf("interfacenaming.New() error = %v", err)
	}
	metricLabels, err := metriccardinality.New(metriccardinality.Options{
		HighCardinalityTypes: []metriccardinality.HighCardinalityType{{
			Package: "fuzzpkg", Name: "Secret",
		}},
		Sinks: []metriccardinality.Sink{{
			Package: "fmt", Symbol: "Printf", VariadicFrom: &variadicFrom,
		}},
	})
	if err != nil {
		tb.Fatalf("metriccardinality.New() error = %v", err)
	}
	metricNames, err := metriccardinality.NewLabelName(
		metriccardinality.LabelNameOptions{
			AttackerControlledTypes: []metriccardinality.AttackerControlledType{{
				Package: "fuzzpkg", Name: "Secret",
			}},
			Sinks: []metriccardinality.Sink{{
				Package: "fmt", Symbol: "Printf", VariadicFrom: &variadicFrom,
			}},
		},
	)
	if err != nil {
		tb.Fatalf("metriccardinality.NewLabelName() error = %v", err)
	}
	backendErrors, err := backenderror.New(backenderror.Options{
		Boundaries: []string{"fuzzpkg"},
		Sources: []backenderror.Flow{{
			Package: "fuzzpkg", Symbol: "Backend", Result: 0,
		}},
	})
	if err != nil {
		tb.Fatalf("backenderror.New() error = %v", err)
	}
	fanout, err := goroutinefanout.New(goroutinefanout.Options{
		Policies: []goroutinefanout.Policy{{Package: "fuzzpkg", MaxStatic: 8}},
	})
	if err != nil {
		tb.Fatalf("goroutinefanout.New() error = %v", err)
	}

	return []*analysis.Analyzer{
		backendErrors,
		blocking,
		cleanup,
		transactions,
		constructors,
		forbidden,
		globalgoroutine.Analyzer,
		fanout,
		httpclienttimeout.Analyzer,
		boundaries,
		lockedCalls,
		nobackground.Analyzer,
		nodefaulthttp.Analyzer,
		noinit.Analyzer,
		noprocesscontrol.Analyzer,
		nostoredcontext.Analyzer,
		nounsafe.Analyzer,
		sensitive,
		mutableGlobals,
		interfaceNames,
		interfaces,
		metricLabels,
		metricNames,
	}
}

func prepareRequirements(
	tb testing.TB,
	pass *analysis.Pass,
	analyzers []*analysis.Analyzer,
) {
	tb.Helper()
	var prepare func(*analysis.Analyzer)
	prepare = func(analyzer *analysis.Analyzer) {
		for _, required := range analyzer.Requires {
			if _, available := pass.ResultOf[required]; available {
				continue
			}
			prepare(required)
			pass.Analyzer = required
			result, err := required.Run(pass)
			if err != nil {
				tb.Fatalf("%s.Run() error = %v", required.Name, err)
			}
			pass.ResultOf[required] = result
		}
	}
	for _, analyzer := range analyzers {
		prepare(analyzer)
	}
}

func buildPass(source string) (*analysis.Pass, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(
		fset,
		"fuzz.go",
		source,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, err
	}
	information := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Instances:  make(map[*ast.Ident]types.Instance),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	checked, err := (&types.Config{Importer: importer.Default()}).Check(
		"fuzzpkg",
		fset,
		[]*ast.File{file},
		information,
	)
	if err != nil {
		return nil, err
	}

	return &analysis.Pass{
		Fset:       fset,
		Files:      []*ast.File{file},
		Pkg:        checked,
		TypesInfo:  information,
		TypesSizes: types.SizesFor("gc", "amd64"),
		ResultOf:   make(map[*analysis.Analyzer]any),
		Report:     func(analysis.Diagnostic) {},
		ImportObjectFact: func(types.Object, analysis.Fact) bool {
			return false
		},
		ExportObjectFact: func(types.Object, analysis.Fact) {},
	}, nil
}

func TestAggregateAllocationBudget(t *testing.T) {
	const allocationBudget = 2200
	if raceEnabled || testing.CoverMode() != "" {
		t.Skip("test instrumentation changes allocation counts")
	}

	pass, err := buildPass(representativeSource)
	if err != nil {
		t.Fatalf("buildPass() error = %v", err)
	}
	analyzers := configuredAnalyzers(t)
	prepareRequirements(t, pass, analyzers)
	allocations := testing.AllocsPerRun(100, func() {
		for _, analyzer := range analyzers {
			pass.Analyzer = analyzer
			if _, err := analyzer.Run(pass); err != nil {
				t.Fatalf("%s.Run() error = %v", analyzer.Name, err)
			}
		}
	})
	if allocations > allocationBudget {
		t.Fatalf(
			"aggregate allocations = %.0f, budget %d",
			allocations,
			allocationBudget,
		)
	}
}

func TestEachAnalyzerAllocationBudget(t *testing.T) {
	if raceEnabled || testing.CoverMode() != "" {
		t.Skip("test instrumentation changes allocation counts")
	}

	budgets := map[string]float64{
		"backenderror":         1900,
		"blockingcontext":      4,
		"cleanupownership":     8,
		"constructorgoroutine": 7,
		"forbiddenapi":         9,
		"globalgoroutine":      6,
		"goroutinefanout":      7,
		"httpclienttimeout":    4,
		"importboundary":       12,
		"interfacenaming":      6,
		"interfaceplacement":   4,
		"lockacrosscall":       70,
		"metriccardinality":    18,
		"metriclabelname":      18,
		"mutableglobal":        4,
		"nobackground":         5,
		"nodefaulthttp":        5,
		"noinit":               3,
		"noprocesscontrol":     4,
		"nostoredcontext":      4,
		"nounsafe":             3,
		"sensitivesink":        16,
		"transactionrollback":  7,
	}

	pass, err := buildPass(representativeSource)
	if err != nil {
		t.Fatalf("buildPass() error = %v", err)
	}
	analyzers := configuredAnalyzers(t)
	prepareRequirements(t, pass, analyzers)
	for _, analyzer := range analyzers {
		budget, exists := budgets[analyzer.Name]
		if !exists {
			t.Errorf("%s has no allocation budget", analyzer.Name)
			continue
		}
		pass.Analyzer = analyzer
		allocations := testing.AllocsPerRun(100, func() {
			if _, err := analyzer.Run(pass); err != nil {
				t.Fatalf("%s.Run() error = %v", analyzer.Name, err)
			}
		})
		if allocations > budget {
			t.Errorf(
				"%s allocations = %.0f, budget %.0f",
				analyzer.Name,
				allocations,
				budget,
			)
		}
	}
	if len(analyzers) != len(budgets) {
		t.Errorf("allocation budgets = %d, analyzers = %d", len(budgets), len(analyzers))
	}
}
