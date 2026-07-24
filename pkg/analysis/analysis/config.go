package analysis

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	maxConfigurationBytes = 1 << 20
	maxGeneratedPaths     = 4096
)

// Config is the versioned organization policy consumed by analyzers.
type Config struct {
	Version                   int                             `yaml:"version" json:"version"`
	Root                      string                          `yaml:"-" json:"root"`
	Packages                  []PackagePolicy                 `yaml:"packages,omitempty" json:"packages,omitempty"`
	Entrypoints               []string                        `yaml:"entrypoints,omitempty" json:"entrypoints,omitempty"`
	InitPackages              []string                        `yaml:"init_packages,omitempty" json:"init_packages,omitempty"`
	ContextOwners             []string                        `yaml:"context_owners,omitempty" json:"context_owners,omitempty"`
	HTTPTimeoutExceptions     []string                        `yaml:"http_timeout_exceptions,omitempty" json:"http_timeout_exceptions,omitempty"`
	Constructors              []ConstructorPolicy             `yaml:"constructors,omitempty" json:"constructors,omitempty"`
	ResourceConstructors      []ResourceConstructorPolicy     `yaml:"resource_constructors,omitempty" json:"resource_constructors,omitempty"`
	Transactions              []TransactionPolicy             `yaml:"transactions,omitempty" json:"transactions,omitempty"`
	LockSensitiveCalls        []LockSensitiveCallPolicy       `yaml:"lock_sensitive_calls,omitempty" json:"lock_sensitive_calls,omitempty"`
	SensitiveTypes            []SensitiveTypePolicy           `yaml:"sensitive_types,omitempty" json:"sensitive_types,omitempty"`
	SensitiveSinks            []SensitiveSinkPolicy           `yaml:"sensitive_sinks,omitempty" json:"sensitive_sinks,omitempty"`
	ForbiddenAPIs             []ForbiddenAPIPolicy            `yaml:"forbidden_apis,omitempty" json:"forbidden_apis,omitempty"`
	BackendClients            []BackendClientPolicy           `yaml:"backend_clients,omitempty" json:"backend_clients,omitempty"`
	MutableGlobals            []MutableGlobalPolicy           `yaml:"mutable_globals,omitempty" json:"mutable_globals,omitempty"`
	InterfaceProviderPackages []string                        `yaml:"interface_provider_packages,omitempty" json:"interface_provider_packages,omitempty"`
	InterfaceNames            []InterfaceNamePolicy           `yaml:"interface_names,omitempty" json:"interface_names,omitempty"`
	MetricLabelTypes          []MetricLabelTypePolicy         `yaml:"metric_label_types,omitempty" json:"metric_label_types,omitempty"`
	MetricLabelSinks          []MetricLabelSinkPolicy         `yaml:"metric_label_sinks,omitempty" json:"metric_label_sinks,omitempty"`
	MetricLabelNameTypes      []MetricLabelNameTypePolicy     `yaml:"metric_label_name_types,omitempty" json:"metric_label_name_types,omitempty"`
	MetricLabelNameSinks      []MetricLabelSinkPolicy         `yaml:"metric_label_name_sinks,omitempty" json:"metric_label_name_sinks,omitempty"`
	BackendErrorBoundaries    []string                        `yaml:"backend_error_boundaries,omitempty" json:"backend_error_boundaries,omitempty"`
	BackendErrorSources       []BackendErrorFlowPolicy        `yaml:"backend_error_sources,omitempty" json:"backend_error_sources,omitempty"`
	BackendErrorPassthroughs  []BackendErrorPassthroughPolicy `yaml:"backend_error_passthroughs,omitempty" json:"backend_error_passthroughs,omitempty"`
	GoroutineFanout           []GoroutineFanoutPolicy         `yaml:"goroutine_fanout,omitempty" json:"goroutine_fanout,omitempty"`
	AdapterRoots              []string                        `yaml:"adapter_roots,omitempty" json:"adapter_roots,omitempty"`
	Layers                    []DirectionPolicy               `yaml:"layers,omitempty" json:"layers,omitempty"`
	Contexts                  []DirectionPolicy               `yaml:"contexts,omitempty" json:"contexts,omitempty"`
	Generated                 GeneratedPolicy                 `yaml:"generated,omitempty" json:"generated,omitempty"`
	Rules                     map[string]RulePolicy           `yaml:"rules,omitempty" json:"rules,omitempty"`
	Exceptions                []PolicyException               `yaml:"exceptions,omitempty" json:"exceptions,omitempty"`
}

// ConstructorPolicy identifies exact constructors with explicit lifecycle rules.
type ConstructorPolicy struct {
	Package string   `yaml:"package" json:"package"`
	Symbols []string `yaml:"symbols" json:"symbols"`
}

// ResourceConstructorPolicy identifies an explicit cleanup ownership contract.
type ResourceConstructorPolicy struct {
	Package         string   `yaml:"package" json:"package"`
	Symbol          string   `yaml:"symbol" json:"symbol"`
	CleanupResult   int      `yaml:"cleanup_result" json:"cleanup_result"`
	AllowedPackages []string `yaml:"allowed_packages,omitempty" json:"allowed_packages,omitempty"`
}

// TransactionPolicy identifies one transaction constructor and its rollback
// ownership contract.
type TransactionPolicy struct {
	Package        string `yaml:"package" json:"package"`
	Symbol         string `yaml:"symbol" json:"symbol"`
	Result         int    `yaml:"result" json:"result"`
	RollbackMethod string `yaml:"rollback_method" json:"rollback_method"`
}

// LockSensitiveCallPolicy identifies a callback or I/O call forbidden under locks.
type LockSensitiveCallPolicy struct {
	Package         string   `yaml:"package" json:"package"`
	Symbol          string   `yaml:"symbol" json:"symbol"`
	AllowedPackages []string `yaml:"allowed_packages,omitempty" json:"allowed_packages,omitempty"`
}

// SensitiveTypePolicy identifies one named secret-bearing type.
type SensitiveTypePolicy struct {
	Package string `yaml:"package" json:"package"`
	Name    string `yaml:"name" json:"name"`
}

// SensitiveSinkPolicy identifies typed-sensitive positions on one callable.
type SensitiveSinkPolicy struct {
	Package         string   `yaml:"package" json:"package"`
	Symbol          string   `yaml:"symbol" json:"symbol"`
	Arguments       []int    `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	VariadicFrom    *int     `yaml:"variadic_from,omitempty" json:"variadic_from,omitempty"`
	AllowedPackages []string `yaml:"allowed_packages,omitempty" json:"allowed_packages,omitempty"`
}

// MetricLabelTypePolicy identifies one named high-cardinality label type.
type MetricLabelTypePolicy struct {
	Package string `yaml:"package" json:"package"`
	Name    string `yaml:"name" json:"name"`
}

// MetricLabelNameTypePolicy identifies one named attacker-controlled label-name
// type.
type MetricLabelNameTypePolicy = MetricLabelTypePolicy

// MetricLabelSinkPolicy identifies label positions on one metric callable.
type MetricLabelSinkPolicy struct {
	Package      string `yaml:"package" json:"package"`
	Symbol       string `yaml:"symbol" json:"symbol"`
	Arguments    []int  `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	VariadicFrom *int   `yaml:"variadic_from,omitempty" json:"variadic_from,omitempty"`
}

// BackendErrorFlowPolicy identifies one backend callable result.
type BackendErrorFlowPolicy struct {
	Package string `yaml:"package" json:"package"`
	Symbol  string `yaml:"symbol" json:"symbol"`
	Result  int    `yaml:"result" json:"result"`
}

// BackendErrorPassthroughPolicy identifies a wrapper that preserves errors.
type BackendErrorPassthroughPolicy struct {
	Package      string `yaml:"package" json:"package"`
	Symbol       string `yaml:"symbol" json:"symbol"`
	Result       int    `yaml:"result" json:"result"`
	Arguments    []int  `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	VariadicFrom *int   `yaml:"variadic_from,omitempty" json:"variadic_from,omitempty"`
}

// GoroutineFanoutPolicy configures a package tree's static fan-out ceiling.
type GoroutineFanoutPolicy struct {
	Package   string `yaml:"package" json:"package"`
	MaxStatic int    `yaml:"max_static" json:"max_static"`
}

// ForbiddenAPIPolicy configures one exact organization API migration.
type ForbiddenAPIPolicy struct {
	Package         string   `yaml:"package" json:"package"`
	Symbol          string   `yaml:"symbol" json:"symbol"`
	Replacement     string   `yaml:"replacement" json:"replacement"`
	AllowedPackages []string `yaml:"allowed_packages,omitempty" json:"allowed_packages,omitempty"`
}

// BackendClientPolicy restricts a backend package tree to approved adapters.
type BackendClientPolicy struct {
	Package         string   `yaml:"package" json:"package"`
	AllowedPackages []string `yaml:"allowed_packages" json:"allowed_packages"`
}

// MutableGlobalPolicy governs composite globals in one package tree.
type MutableGlobalPolicy struct {
	Package string `yaml:"package" json:"package"`
}

// InterfaceNamePolicy governs exported value-interface names in one package tree.
type InterfaceNamePolicy struct {
	Package        string   `yaml:"package" json:"package"`
	RequiredPrefix string   `yaml:"required_prefix,omitempty" json:"required_prefix,omitempty"`
	RequiredSuffix string   `yaml:"required_suffix,omitempty" json:"required_suffix,omitempty"`
	AllowedNames   []string `yaml:"allowed_names,omitempty" json:"allowed_names,omitempty"`
}

// PackagePolicy assigns architecture constraints to matching import paths.
type PackagePolicy struct {
	Pattern           string   `yaml:"pattern" json:"pattern"`
	Layer             string   `yaml:"layer,omitempty" json:"layer,omitempty"`
	Context           string   `yaml:"context,omitempty" json:"context,omitempty"`
	AllowImports      []string `yaml:"allow_imports,omitempty" json:"allow_imports,omitempty"`
	DenyImports       []string `yaml:"deny_imports,omitempty" json:"deny_imports,omitempty"`
	BlockingFunctions []string `yaml:"blocking_functions,omitempty" json:"blocking_functions,omitempty"`
}

// DirectionPolicy names one architecture partition and permitted dependencies.
type DirectionPolicy struct {
	Name      string   `yaml:"name" json:"name"`
	MayImport []string `yaml:"may_import,omitempty" json:"may_import,omitempty"`
}

// GeneratedPolicy controls exact trusted generated Go file exclusions.
type GeneratedPolicy struct {
	Exclude bool     `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Paths   []string `yaml:"paths,omitempty" json:"paths,omitempty"`
}

// RulePolicy overrides metadata defaults for one known rule.
type RulePolicy struct {
	Status    Status           `yaml:"status,omitempty" json:"status,omitempty"`
	Severity  Severity         `yaml:"severity,omitempty" json:"severity,omitempty"`
	Options   map[string]any   `yaml:"options,omitempty" json:"options,omitempty"`
	Promotion *PromotionPolicy `yaml:"promotion,omitempty" json:"promotion,omitempty"`
}

// PromotionPolicy proves that one blocking override is explicit and versioned.
type PromotionPolicy struct {
	Version  string `yaml:"version" json:"version"`
	Evidence string `yaml:"evidence" json:"evidence"`
}

// PolicyException is a narrow, reviewed exception to one rule.
type PolicyException struct {
	Rule    string `yaml:"rule" json:"rule"`
	Package string `yaml:"package" json:"package"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	Reason  string `yaml:"reason" json:"reason"`
	Expires string `yaml:"expires,omitempty" json:"expires,omitempty"`
	Issue   string `yaml:"issue,omitempty" json:"issue,omitempty"`
	Used    bool   `yaml:"-" json:"used"`
}

// LoadConfig reads exactly one strict YAML policy document.
func LoadConfig(path string, knownRules []string) (*Config, error) {
	return loadConfig(path, knownRules, filepath.Abs)
}

func loadConfig(
	path string,
	knownRules []string,
	resolve func(string) (string, error),
) (*Config, error) {
	absolute, err := resolve(path)
	if err != nil {
		return nil, fmt.Errorf("resolve configuration path: %w", err)
	}
	contents, err := readConfiguration(absolute)
	if err != nil {
		return nil, err
	}

	return decodeConfig(contents, filepath.Dir(absolute), knownRules)
}

func readConfiguration(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- explicit policy path
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	defer func() { _ = file.Close() }()

	contents, err := io.ReadAll(io.LimitReader(file, maxConfigurationBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	if len(contents) > maxConfigurationBytes {
		return nil, fmt.Errorf("configuration exceeds %d bytes", maxConfigurationBytes)
	}

	return contents, nil
}

func decodeConfig(contents []byte, root string, knownRules []string) (*Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode trailing configuration: %w", err)
		}
		return nil, errors.New("configuration must contain exactly one document")
	}

	config.Root = root
	if err := config.Validate(knownRules); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate checks a decoded configuration against the governed rule IDs.
func (config Config) Validate(knownRules []string) error {
	if config.Version != 1 {
		return fmt.Errorf("unsupported configuration version %d", config.Version)
	}
	known := make(map[string]struct{}, len(knownRules))
	for _, rule := range knownRules {
		known[rule] = struct{}{}
	}
	for rule, policy := range config.Rules {
		if _, ok := known[rule]; !ok {
			return fmt.Errorf("unknown rule %q", rule)
		}
		if policy.Status != "" && !validStatus(policy.Status) {
			return fmt.Errorf("rule %q has unknown status %q", rule, policy.Status)
		}
		if policy.Severity != "" && !validSeverity(policy.Severity) {
			return fmt.Errorf("rule %q has unknown severity %q", rule, policy.Severity)
		}
		if policy.Options != nil {
			return fmt.Errorf("rule %q options are unsupported; use its typed top-level policy", rule)
		}
		if policy.Status == StatusBlocking {
			if policy.Promotion == nil {
				return fmt.Errorf("blocking rule %q requires promotion evidence", rule)
			}
			if !versionPattern.MatchString(policy.Promotion.Version) {
				return fmt.Errorf("blocking rule %q requires a semantic promotion version", rule)
			}
			if strings.TrimSpace(policy.Promotion.Evidence) == "" {
				return fmt.Errorf("blocking rule %q requires non-empty promotion evidence", rule)
			}
		} else if policy.Promotion != nil {
			return fmt.Errorf("non-blocking rule %q must not declare promotion evidence", rule)
		}
	}
	if config.AdapterRoots != nil {
		return errors.New("adapter_roots are unsupported without a typed backend client policy")
	}
	if config.Generated.Exclude && len(config.Generated.Paths) == 0 {
		return errors.New("generated exclusion requires exact paths")
	}
	if !config.Generated.Exclude && config.Generated.Paths != nil {
		return errors.New("generated paths require exclusion to be enabled")
	}
	if len(config.Generated.Paths) > maxGeneratedPaths {
		return fmt.Errorf("generated exclusion exceeds %d paths", maxGeneratedPaths)
	}
	generatedPaths := make(map[string]struct{}, len(config.Generated.Paths))
	for index, generatedPath := range config.Generated.Paths {
		if !validGeneratedPath(generatedPath) {
			return fmt.Errorf(
				"generated path %d requires an exact repository-relative Go file",
				index,
			)
		}
		if _, exists := generatedPaths[generatedPath]; exists {
			return fmt.Errorf("generated path %d duplicates %q", index, generatedPath)
		}
		generatedPaths[generatedPath] = struct{}{}
	}
	for index, exception := range config.Exceptions {
		if _, ok := known[exception.Rule]; !ok {
			return fmt.Errorf("exception %d names unknown rule %q", index, exception.Rule)
		}
		if !validExceptionPackage(exception.Package) {
			return fmt.Errorf("exception %d requires an exact clean package", index)
		}
		if exception.Path != "" && !validExceptionPath(exception.Path) {
			return fmt.Errorf("exception %d requires an exact repository-relative path", index)
		}
		if strings.TrimSpace(exception.Reason) == "" {
			return fmt.Errorf("exception %d requires a reason", index)
		}
		if exception.Expires != "" {
			if _, err := time.Parse("2006-01-02", exception.Expires); err != nil {
				return fmt.Errorf("exception %d has invalid expiry: %w", index, err)
			}
		}
		if exception.Issue != "" && strings.TrimSpace(exception.Issue) == "" {
			return fmt.Errorf("exception %d has an empty issue", index)
		}
		for previous := range index {
			other := config.Exceptions[previous]
			if other.Rule == exception.Rule && other.Package == exception.Package &&
				(other.Path == exception.Path || other.Path == "" || exception.Path == "") {
				return fmt.Errorf("exception %d overlaps exception %d", index, previous)
			}
		}
	}

	return nil
}

func validGeneratedPath(value string) bool {
	return validExceptionPath(value) && strings.HasSuffix(value, ".go") &&
		!strings.ContainsAny(value, "*") && !strings.Contains(value, "...")
}
