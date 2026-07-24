// Package driver loads Go packages and executes configured analyzers once.
package driver

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
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
	"github.com/faustbrian/golib/pkg/analysis/internal/version"
	"github.com/faustbrian/golib/pkg/analysis/policy"
	toolanalysis "golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

const maxReportEntries = 100_000

// Options controls one complete analysis invocation.
type Options struct {
	ConfigPath string
	Root       string
	Patterns   []string
	Sequential bool
	Now        time.Time
}

// Result contains normalized findings and whether blocking policy failed.
type Result struct {
	Report   shared.Report
	Blocking bool
}

type analyzerSpec struct {
	analyzer *toolanalysis.Analyzer
	rule     shared.Rule
	status   shared.Status
}

type diagnosticBudget struct {
	limit    int64
	emitted  atomic.Int64
	overflow atomic.Bool
}

func newDiagnosticBudget(limit int64) *diagnosticBudget {
	return &diagnosticBudget{limit: limit}
}

func (budget *diagnosticBudget) wrap(
	analyzer *toolanalysis.Analyzer,
) *toolanalysis.Analyzer {
	wrapped := *analyzer
	run := analyzer.Run
	wrapped.Run = func(pass *toolanalysis.Pass) (any, error) {
		limited := *pass
		report := pass.Report
		limited.Report = func(diagnostic toolanalysis.Diagnostic) {
			if budget.emitted.Add(1) > budget.limit {
				budget.overflow.Store(true)
				return
			}
			report(diagnostic)
		}
		return run(&limited)
	}
	return &wrapped
}

func (budget *diagnosticBudget) exceeded() bool {
	return budget.overflow.Load()
}

type dependencies struct {
	registry        func() (*policy.Registry, error)
	load            func(*packages.Config, ...string) ([]*packages.Package, error)
	analyze         func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error)
	now             func() time.Time
	relative        func(string, string) (string, error)
	diagnosticLimit int64
}

// Run loads target packages once, analyzes them, and applies suppressions.
func Run(ctx context.Context, options Options) (Result, error) {
	return run(ctx, options, dependencies{
		registry:        policy.Builtin,
		load:            packages.Load,
		analyze:         checker.Analyze,
		now:             time.Now,
		relative:        repositoryPath,
		diagnosticLimit: maxReportEntries,
	})
}

// Validate checks all analyzer-specific configuration without loading packages.
func Validate(config *shared.Config) error {
	return validate(config, policy.Builtin)
}

func validate(
	config *shared.Config,
	registryFactory func() (*policy.Registry, error),
) error {
	if config == nil {
		return errors.New("configuration is required")
	}
	registry, err := registryFactory()
	if err != nil {
		return fmt.Errorf("build rule registry: %w", err)
	}
	if err := config.Validate(registry.IDs()); err != nil {
		return err
	}
	_, err = buildSpecs(config)
	return err
}

func run(ctx context.Context, options Options, dependencies dependencies) (Result, error) {
	registry, err := dependencies.registry()
	if err != nil {
		return Result{}, fmt.Errorf("build rule registry: %w", err)
	}
	config, err := shared.LoadConfig(options.ConfigPath, registry.IDs())
	if err != nil {
		return Result{}, err
	}
	if options.Root != "" {
		if !filepath.IsAbs(options.Root) {
			return Result{}, errors.New("analysis root must be absolute")
		}
		config.Root = filepath.Clean(options.Root)
	}
	specs, err := buildSpecs(config)
	if err != nil {
		return Result{}, err
	}
	patterns := options.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	loaded, err := dependencies.load(&packages.Config{
		Context: ctx,
		Mode:    packages.LoadSyntax,
		Dir:     config.Root,
	}, patterns...)
	if err != nil {
		return Result{}, fmt.Errorf("load packages: %w", err)
	}
	if len(loaded) == 0 {
		return Result{}, errors.New("package patterns matched no packages")
	}
	if err := packageErrors(loaded); err != nil {
		return Result{}, err
	}

	diagnosticLimit := dependencies.diagnosticLimit
	if diagnosticLimit == 0 {
		diagnosticLimit = maxReportEntries
	}
	budget := newDiagnosticBudget(diagnosticLimit)
	analyzers := make([]*toolanalysis.Analyzer, 0, len(specs))
	byAnalyzer := make(map[*toolanalysis.Analyzer]analyzerSpec, len(specs))
	statusByRule := make(map[string]shared.Status, len(specs))
	for _, spec := range specs {
		if spec.status == shared.StatusDisabled {
			continue
		}
		wrapped := budget.wrap(spec.analyzer)
		spec.analyzer = wrapped
		analyzers = append(analyzers, wrapped)
		byAnalyzer[wrapped] = spec
		statusByRule[spec.rule.ID] = spec.status
	}
	graph, err := dependencies.analyze(analyzers, loaded, &checker.Options{
		Sequential:  options.Sequential,
		SanityCheck: true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("analyze packages: %w", err)
	}
	if budget.exceeded() {
		return Result{}, fmt.Errorf("diagnostics exceed %d entries", diagnosticLimit)
	}

	excludedFiles := generatedFiles(
		loaded,
		config.Generated.Exclude,
		config.Root,
		config.Generated.Paths,
	)
	diagnostics, err := collectDiagnostics(graph, byAnalyzer, excludedFiles)
	if err != nil {
		return Result{}, err
	}
	now := options.Now
	if now.IsZero() {
		now = dependencies.now().UTC()
	}
	diagnostics, exceptions, err := shared.ApplyPolicyExceptions(
		config.Root,
		diagnostics,
		config.Exceptions,
		now,
	)
	if err != nil {
		return Result{}, err
	}
	suppressions, err := collectSuppressions(
		loaded,
		registry.IDs(),
		now,
		excludedFiles,
	)
	if err != nil {
		return Result{}, err
	}
	diagnostics, suppressions, err = shared.ApplySuppressions(diagnostics, suppressions)
	if err != nil {
		return Result{}, err
	}
	if err := relativize(config.Root, diagnostics, suppressions, dependencies.relative); err != nil {
		return Result{}, err
	}

	report := shared.Report{
		ToolVersion:  version.Value,
		Root:         config.Root,
		Rules:        registryRules(registry),
		Diagnostics:  diagnostics,
		Exceptions:   exceptions,
		Suppressions: suppressions,
	}
	result := Result{Report: report}
	for _, diagnostic := range diagnostics {
		if statusByRule[diagnostic.Rule] == shared.StatusBlocking {
			result.Blocking = true
			break
		}
	}

	return result, nil
}

func buildSpecs(config *shared.Config) ([]analyzerSpec, error) {
	fanoutPolicies := make([]goroutinefanout.Policy, 0, len(config.GoroutineFanout))
	for _, policy := range config.GoroutineFanout {
		fanoutPolicies = append(fanoutPolicies, goroutinefanout.Policy{
			Package: policy.Package, MaxStatic: policy.MaxStatic,
		})
	}
	backendSources := make([]backenderror.Flow, 0, len(config.BackendErrorSources))
	for _, source := range config.BackendErrorSources {
		backendSources = append(backendSources, backenderror.Flow{
			Package: source.Package, Symbol: source.Symbol, Result: source.Result,
		})
	}
	backendPassthroughs := make(
		[]backenderror.Passthrough,
		0,
		len(config.BackendErrorPassthroughs),
	)
	for _, passthrough := range config.BackendErrorPassthroughs {
		backendPassthroughs = append(backendPassthroughs, backenderror.Passthrough{
			Package:      passthrough.Package,
			Symbol:       passthrough.Symbol,
			Result:       passthrough.Result,
			Arguments:    passthrough.Arguments,
			VariadicFrom: passthrough.VariadicFrom,
		})
	}
	boundaries := make([]importboundary.Policy, 0, len(config.Packages))
	restrictedDependencies := make(
		[]importboundary.RestrictedDependency,
		0,
		len(config.BackendClients),
	)
	for _, client := range config.BackendClients {
		restrictedDependencies = append(
			restrictedDependencies,
			importboundary.RestrictedDependency{
				Package:         client.Package,
				AllowedPackages: client.AllowedPackages,
			},
		)
	}
	mutableGlobalPolicies := make([]mutableglobal.Policy, 0, len(config.MutableGlobals))
	for _, configured := range config.MutableGlobals {
		mutableGlobalPolicies = append(mutableGlobalPolicies, mutableglobal.Policy{
			Package: configured.Package,
		})
	}
	packageClasses := make([]importboundary.PackageClass, 0, len(config.Packages))
	blockingAPIs := make([]blockingcontext.Policy, 0, len(config.Packages))
	constructors := make([]constructorgoroutine.Policy, 0, len(config.Constructors))
	for _, constructor := range config.Constructors {
		constructors = append(constructors, constructorgoroutine.Policy{
			Package: constructor.Package,
			Symbols: constructor.Symbols,
		})
	}
	resourceConstructors := make(
		[]cleanupownership.Constructor,
		0,
		len(config.ResourceConstructors),
	)
	for _, constructor := range config.ResourceConstructors {
		resourceConstructors = append(resourceConstructors, cleanupownership.Constructor{
			Package:         constructor.Package,
			Symbol:          constructor.Symbol,
			CleanupResult:   constructor.CleanupResult,
			AllowedPackages: constructor.AllowedPackages,
		})
	}
	transactions := make(
		[]transactionrollback.Transaction,
		0,
		len(config.Transactions),
	)
	for _, transaction := range config.Transactions {
		transactions = append(transactions, transactionrollback.Transaction{
			Package:        transaction.Package,
			Symbol:         transaction.Symbol,
			Result:         transaction.Result,
			RollbackMethod: transaction.RollbackMethod,
		})
	}
	lockSensitiveCalls := make(
		[]lockacrosscall.Call,
		0,
		len(config.LockSensitiveCalls),
	)
	for _, call := range config.LockSensitiveCalls {
		lockSensitiveCalls = append(lockSensitiveCalls, lockacrosscall.Call{
			Package:         call.Package,
			Symbol:          call.Symbol,
			AllowedPackages: call.AllowedPackages,
		})
	}
	forbiddenAPIs := make([]forbiddenapi.Policy, 0, len(config.ForbiddenAPIs))
	for _, api := range config.ForbiddenAPIs {
		forbiddenAPIs = append(forbiddenAPIs, forbiddenapi.Policy{
			Package:         api.Package,
			Symbol:          api.Symbol,
			Replacement:     api.Replacement,
			AllowedPackages: api.AllowedPackages,
		})
	}
	sensitiveTypes := make([]sensitivesink.SensitiveType, 0, len(config.SensitiveTypes))
	for _, configuredType := range config.SensitiveTypes {
		sensitiveTypes = append(sensitiveTypes, sensitivesink.SensitiveType{
			Package: configuredType.Package,
			Name:    configuredType.Name,
		})
	}
	sensitiveSinks := make([]sensitivesink.Sink, 0, len(config.SensitiveSinks))
	for _, sink := range config.SensitiveSinks {
		sensitiveSinks = append(sensitiveSinks, sensitivesink.Sink{
			Package:         sink.Package,
			Symbol:          sink.Symbol,
			Arguments:       sink.Arguments,
			VariadicFrom:    sink.VariadicFrom,
			AllowedPackages: sink.AllowedPackages,
		})
	}
	metricTypes := make(
		[]metriccardinality.AttackerControlledType,
		0,
		len(config.MetricLabelTypes),
	)
	for _, configuredType := range config.MetricLabelTypes {
		metricTypes = append(metricTypes, metriccardinality.HighCardinalityType{
			Package: configuredType.Package,
			Name:    configuredType.Name,
		})
	}
	metricSinks := make([]metriccardinality.Sink, 0, len(config.MetricLabelSinks))
	for _, sink := range config.MetricLabelSinks {
		metricSinks = append(metricSinks, metriccardinality.Sink{
			Package:      sink.Package,
			Symbol:       sink.Symbol,
			Arguments:    sink.Arguments,
			VariadicFrom: sink.VariadicFrom,
		})
	}
	metricNameTypes := make(
		[]metriccardinality.HighCardinalityType,
		0,
		len(config.MetricLabelNameTypes),
	)
	for _, configuredType := range config.MetricLabelNameTypes {
		metricNameTypes = append(metricNameTypes, metriccardinality.AttackerControlledType{
			Package: configuredType.Package,
			Name:    configuredType.Name,
		})
	}
	metricNameSinks := make([]metriccardinality.Sink, 0, len(config.MetricLabelNameSinks))
	for _, sink := range config.MetricLabelNameSinks {
		metricNameSinks = append(metricNameSinks, metriccardinality.Sink{
			Package:      sink.Package,
			Symbol:       sink.Symbol,
			Arguments:    sink.Arguments,
			VariadicFrom: sink.VariadicFrom,
		})
	}
	for _, packagePolicy := range config.Packages {
		if len(packagePolicy.AllowImports) > 0 && len(packagePolicy.DenyImports) == 0 {
			return nil, fmt.Errorf("package policy %q allows imports without a deny policy",
				packagePolicy.Pattern)
		}
		if packagePolicy.Layer == "" && packagePolicy.Context == "" &&
			len(packagePolicy.DenyImports) == 0 &&
			len(packagePolicy.BlockingFunctions) == 0 {
			return nil, fmt.Errorf("package policy %q has no enforceable policy",
				packagePolicy.Pattern)
		}
		if packagePolicy.Layer != "" || packagePolicy.Context != "" {
			packageClasses = append(packageClasses, importboundary.PackageClass{
				Package: packagePolicy.Pattern,
				Layer:   packagePolicy.Layer,
				Context: packagePolicy.Context,
			})
		}
		if len(packagePolicy.DenyImports) > 0 {
			boundaries = append(boundaries, importboundary.Policy{
				Package:      packagePolicy.Pattern,
				DenyImports:  packagePolicy.DenyImports,
				AllowImports: packagePolicy.AllowImports,
			})
		}
		if len(packagePolicy.BlockingFunctions) > 0 {
			blockingAPIs = append(blockingAPIs, blockingcontext.Policy{
				Package:   packagePolicy.Pattern,
				Functions: packagePolicy.BlockingFunctions,
			})
		}
	}
	blockingAnalyzer, err := blockingcontext.New(blockingcontext.Options{
		Policies: blockingAPIs,
	})
	if err != nil {
		return nil, fmt.Errorf("configure blocking context analyzer: %w", err)
	}
	cleanupAnalyzer, err := cleanupownership.New(cleanupownership.Options{
		Constructors: resourceConstructors,
	})
	if err != nil {
		return nil, fmt.Errorf("configure cleanup ownership analyzer: %w", err)
	}
	transactionAnalyzer, err := transactionrollback.New(transactionrollback.Options{
		Transactions: transactions,
	})
	if err != nil {
		return nil, fmt.Errorf("configure transaction rollback analyzer: %w", err)
	}
	constructorAnalyzer, err := constructorgoroutine.New(
		constructorgoroutine.Options{Policies: constructors},
	)
	if err != nil {
		return nil, fmt.Errorf("configure constructor goroutine analyzer: %w", err)
	}
	forbiddenAnalyzer, err := forbiddenapi.New(forbiddenapi.Options{
		Policies: forbiddenAPIs,
	})
	if err != nil {
		return nil, fmt.Errorf("configure forbidden API analyzer: %w", err)
	}
	timeoutAnalyzer, err := httpclienttimeout.New(httpclienttimeout.Options{
		AllowedPackages: config.HTTPTimeoutExceptions,
	})
	if err != nil {
		return nil, fmt.Errorf("configure HTTP client timeout analyzer: %w", err)
	}
	lockAnalyzer, err := lockacrosscall.New(lockacrosscall.Options{
		Calls: lockSensitiveCalls,
	})
	if err != nil {
		return nil, fmt.Errorf("configure lock-sensitive call analyzer: %w", err)
	}
	sensitiveAnalyzer, err := sensitivesink.New(sensitivesink.Options{
		SensitiveTypes: sensitiveTypes,
		Sinks:          sensitiveSinks,
	})
	if err != nil {
		return nil, fmt.Errorf("configure sensitive sink analyzer: %w", err)
	}
	boundaryAnalyzer, err := importboundary.New(importboundary.Options{
		Policies:               boundaries,
		RestrictedDependencies: restrictedDependencies,
		Packages:               packageClasses,
		Layers:                 directions(config.Layers),
		Contexts:               directions(config.Contexts),
	})
	if err != nil {
		return nil, fmt.Errorf("configure import boundary analyzer: %w", err)
	}
	mutableGlobalAnalyzer, err := mutableglobal.New(mutableglobal.Options{
		Policies: mutableGlobalPolicies,
	})
	if err != nil {
		return nil, fmt.Errorf("configure mutable global analyzer: %w", err)
	}
	interfaceAnalyzer, err := interfaceplacement.New(interfaceplacement.Options{
		Packages: config.InterfaceProviderPackages,
	})
	if err != nil {
		return nil, fmt.Errorf("configure interface placement analyzer: %w", err)
	}
	interfaceNamePolicies := make([]interfacenaming.Policy, 0, len(config.InterfaceNames))
	for _, configured := range config.InterfaceNames {
		interfaceNamePolicies = append(interfaceNamePolicies, interfacenaming.Policy{
			Package:        configured.Package,
			RequiredPrefix: configured.RequiredPrefix,
			RequiredSuffix: configured.RequiredSuffix,
			AllowedNames:   configured.AllowedNames,
		})
	}
	interfaceNameAnalyzer, err := interfacenaming.New(interfacenaming.Options{
		Policies: interfaceNamePolicies,
	})
	if err != nil {
		return nil, fmt.Errorf("configure interface naming analyzer: %w", err)
	}
	metricAnalyzer, err := metriccardinality.New(metriccardinality.Options{
		HighCardinalityTypes: metricTypes,
		Sinks:                metricSinks,
	})
	if err != nil {
		return nil, fmt.Errorf("configure metric cardinality analyzer: %w", err)
	}
	metricNameAnalyzer, err := metriccardinality.NewLabelName(
		metriccardinality.LabelNameOptions{
			AttackerControlledTypes: metricNameTypes,
			Sinks:                   metricNameSinks,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("configure metric label name analyzer: %w", err)
	}
	backendErrorAnalyzer, err := backenderror.New(backenderror.Options{
		Boundaries:   config.BackendErrorBoundaries,
		Sources:      backendSources,
		Passthroughs: backendPassthroughs,
	})
	if err != nil {
		return nil, fmt.Errorf("configure backend error analyzer: %w", err)
	}
	fanoutAnalyzer, err := goroutinefanout.New(goroutinefanout.Options{
		Policies: fanoutPolicies,
	})
	if err != nil {
		return nil, fmt.Errorf("configure goroutine fan-out analyzer: %w", err)
	}
	specs := []analyzerSpec{
		{analyzer: backendErrorAnalyzer, rule: backenderror.Rule},
		{analyzer: blockingAnalyzer, rule: blockingcontext.Rule},
		{analyzer: cleanupAnalyzer, rule: cleanupownership.Rule},
		{analyzer: transactionAnalyzer, rule: transactionrollback.Rule},
		{analyzer: constructorAnalyzer, rule: constructorgoroutine.Rule},
		{analyzer: forbiddenAnalyzer, rule: forbiddenapi.Rule},
		{analyzer: globalgoroutine.Analyzer, rule: globalgoroutine.Rule},
		{analyzer: timeoutAnalyzer, rule: httpclienttimeout.Rule},
		{analyzer: boundaryAnalyzer, rule: importboundary.Rule},
		{analyzer: lockAnalyzer, rule: lockacrosscall.Rule},
		{
			analyzer: nobackground.New(nobackground.Options{
				AllowedPackages: config.Entrypoints,
			}),
			rule: nobackground.Rule,
		},
		{analyzer: nodefaulthttp.Analyzer, rule: nodefaulthttp.Rule},
		{
			analyzer: noinit.New(noinit.Options{
				AllowedPackages: config.InitPackages,
			}),
			rule: noinit.Rule,
		},
		{
			analyzer: noprocesscontrol.New(noprocesscontrol.Options{
				AllowedPackages: config.Entrypoints,
			}),
			rule: noprocesscontrol.Rule,
		},
		{
			analyzer: nostoredcontext.New(nostoredcontext.Options{
				AllowedPackages: config.ContextOwners,
			}),
			rule: nostoredcontext.Rule,
		},
		{analyzer: sensitiveAnalyzer, rule: sensitivesink.Rule},
		{analyzer: nounsafe.Analyzer, rule: nounsafe.Rule},
		{analyzer: mutableGlobalAnalyzer, rule: mutableglobal.Rule},
		{analyzer: interfaceNameAnalyzer, rule: interfacenaming.Rule},
		{analyzer: interfaceAnalyzer, rule: interfaceplacement.Rule},
		{analyzer: metricAnalyzer, rule: metriccardinality.Rule},
		{analyzer: metricNameAnalyzer, rule: metriccardinality.LabelNameRule},
		{analyzer: fanoutAnalyzer, rule: goroutinefanout.Rule},
	}
	for index := range specs {
		specs[index].status = specs[index].rule.DefaultStatus
		if configured, ok := config.Rules[specs[index].rule.ID]; ok {
			if configured.Status != "" {
				specs[index].status = configured.Status
			}
			if configured.Severity != "" {
				specs[index].rule.Severity = configured.Severity
			}
		}
	}

	return specs, nil
}

func directions(configured []shared.DirectionPolicy) []importboundary.Direction {
	result := make([]importboundary.Direction, 0, len(configured))
	for _, direction := range configured {
		result = append(result, importboundary.Direction{
			Name:      direction.Name,
			MayImport: direction.MayImport,
		})
	}

	return result
}

func packageErrors(loaded []*packages.Package) error {
	var messages []string
	for _, loadedPackage := range loaded {
		for _, packageError := range loadedPackage.Errors {
			messages = append(messages, packageError.Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	sort.Strings(messages)

	return fmt.Errorf("load package syntax: %s", strings.Join(messages, "; "))
}

func collectDiagnostics(
	graph *checker.Graph,
	byAnalyzer map[*toolanalysis.Analyzer]analyzerSpec,
	excludedFiles map[string]struct{},
) ([]shared.Diagnostic, error) {
	return collectDiagnosticsLimited(graph, byAnalyzer, excludedFiles, maxReportEntries)
}

func collectDiagnosticsLimited(
	graph *checker.Graph,
	byAnalyzer map[*toolanalysis.Analyzer]analyzerSpec,
	excludedFiles map[string]struct{},
	limit int,
) ([]shared.Diagnostic, error) {
	var diagnostics []shared.Diagnostic
	for _, action := range graph.Roots {
		if action.Err != nil {
			return nil, fmt.Errorf("run %s on %s: %w",
				action.Analyzer.Name, action.Package.PkgPath, action.Err)
		}
		spec := byAnalyzer[action.Analyzer]
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			if _, excluded := excludedFiles[position.Filename]; excluded {
				continue
			}
			if err := ensureReportCapacity(limit, len(diagnostics), 1, "diagnostics"); err != nil {
				return nil, err
			}
			diagnostics = append(diagnostics, shared.Diagnostic{
				Rule:        spec.rule.ID,
				Package:     action.Package.PkgPath,
				Filename:    position.Filename,
				Line:        position.Line,
				Column:      position.Column,
				Message:     diagnostic.Message,
				Rationale:   spec.rule.Rationale,
				Remediation: spec.rule.Remediation,
			})
		}
	}

	return diagnostics, nil
}

func collectSuppressions(
	loaded []*packages.Package,
	knownRules []string,
	now time.Time,
	excludedFiles map[string]struct{},
) ([]shared.Suppression, error) {
	return collectSuppressionsLimited(loaded, knownRules, now, excludedFiles, maxReportEntries)
}

func collectSuppressionsLimited(
	loaded []*packages.Package,
	knownRules []string,
	now time.Time,
	excludedFiles map[string]struct{},
	limit int,
) ([]shared.Suppression, error) {
	var suppressions []shared.Suppression
	for _, loadedPackage := range loaded {
		for _, file := range loadedPackage.Syntax {
			filename := loadedPackage.Fset.Position(file.Pos()).Filename
			if _, excluded := excludedFiles[filename]; excluded {
				continue
			}
			parsed, err := shared.ParseSuppressions(
				loadedPackage.Fset,
				file,
				knownRules,
				now,
			)
			if err != nil {
				return nil, err
			}
			if err := ensureReportCapacity(
				limit,
				len(suppressions),
				len(parsed),
				"suppressions",
			); err != nil {
				return nil, err
			}
			suppressions = append(suppressions, parsed...)
		}
	}

	return suppressions, nil
}

func ensureReportCapacity(limit, current, additional int, kind string) error {
	if limit < 0 || current < 0 || additional < 0 {
		return fmt.Errorf("invalid %s report entry count", kind)
	}
	if current > limit-additional {
		return fmt.Errorf("%s exceed %d report entries", kind, limit)
	}

	return nil
}

func generatedFiles(
	loaded []*packages.Package,
	exclude bool,
	root string,
	paths []string,
) map[string]struct{} {
	if !exclude {
		return nil
	}
	trusted := make(map[string]struct{}, len(paths))
	for _, relative := range paths {
		trusted[filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))] = struct{}{}
	}
	generated := make(map[string]struct{})
	for _, loadedPackage := range loaded {
		for _, file := range loadedPackage.Syntax {
			filename := filepath.Clean(loadedPackage.Fset.Position(file.Pos()).Filename)
			if _, allowed := trusted[filename]; allowed && ast.IsGenerated(file) {
				generated[filename] = struct{}{}
			}
		}
	}
	return generated
}

func relativize(
	root string,
	diagnostics []shared.Diagnostic,
	suppressions []shared.Suppression,
	relativePath func(string, string) (string, error),
) error {
	for index := range diagnostics {
		relative, err := relativePath(root, diagnostics[index].Filename)
		if err != nil {
			return err
		}
		diagnostics[index].Filename = relative
	}
	for index := range suppressions {
		relative, err := relativePath(root, suppressions[index].Filename)
		if err != nil {
			return err
		}
		suppressions[index].Filename = relative
	}

	return nil
}

func repositoryPath(root, filename string) (string, error) {
	return repositoryPathWithRel(root, filename, filepath.Rel)
}

func repositoryPathWithRel(
	root string,
	filename string,
	rel func(string, string) (string, error),
) (string, error) {
	relative, err := rel(root, filename)
	if err != nil {
		return "", fmt.Errorf("make diagnostic path relative: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("diagnostic path escapes repository root")
	}

	return filepath.ToSlash(relative), nil
}

func registryRules(registry *policy.Registry) []shared.Rule {
	entries := registry.Entries()
	rules := make([]shared.Rule, 0, len(entries))
	for _, entry := range entries {
		rules = append(rules, entry.Rule)
	}

	return rules
}
