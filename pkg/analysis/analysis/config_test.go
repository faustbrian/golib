package analysis_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func TestLoadConfigUsesConfigDirectoryAsDeterministicRoot(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := writeConfig(t, directory, `
version: 1
generated:
  exclude: true
  paths:
    - generated/client.go
init_packages:
  - github.com/acme/service/cmd/service
context_owners:
  - github.com/acme/service/internal/request
http_timeout_exceptions:
  - github.com/acme/service/internal/streaming
constructors:
  - package: github.com/acme/service/internal/worker
    symbols:
      - New
resource_constructors:
  - package: github.com/acme/service/internal/store
    symbol: Open
    cleanup_result: 1
    allowed_packages:
      - github.com/acme/service/internal/runtime
transactions:
  - package: database/sql
    symbol: DB.BeginTx
    result: 0
    rollback_method: Rollback
lock_sensitive_calls:
  - package: github.com/acme/service/internal/backend
    symbol: Client.Call
    allowed_packages:
      - github.com/acme/service/internal/adapter
sensitive_types:
  - package: github.com/acme/security
    name: Token
sensitive_sinks:
  - package: log/slog
    symbol: Logger.Log
    arguments: [2]
    variadic_from: 3
    allowed_packages:
      - github.com/acme/service/internal/audit
forbidden_apis:
  - package: github.com/acme/legacy
    symbol: Client.Call
    replacement: github.com/acme/ports.Client.Call
    allowed_packages:
      - github.com/acme/service/internal/adapter
backend_clients:
  - package: github.com/acme/backend/...
    allowed_packages:
      - github.com/acme/service/internal/adapter/...
mutable_globals:
  - package: github.com/acme/service/internal/runtime/...
interface_provider_packages:
  - github.com/acme/service/internal/provider/...
interface_names:
  - package: github.com/acme/service/internal/ports/...
    required_prefix: Order
    required_suffix: Port
    allowed_names: [Compatibility]
metric_label_types:
  - package: github.com/acme/service/internal/model
    name: UserID
metric_label_sinks:
  - package: github.com/acme/metrics
    symbol: Counter.Label
    arguments: [0]
metric_label_name_types:
  - package: github.com/acme/service/internal/request
    name: MetricName
metric_label_name_sinks:
  - package: github.com/acme/metrics
    symbol: Counter.LabelName
    arguments: [0]
backend_error_boundaries:
  - github.com/acme/service/api/...
backend_error_sources:
  - package: github.com/acme/backend
    symbol: Client.Load
    result: 1
backend_error_passthroughs:
  - package: fmt
    symbol: Errorf
    result: 0
    variadic_from: 1
goroutine_fanout:
  - package: github.com/acme/service/internal/worker/...
    max_static: 8
layers:
  - name: domain
    may_import:
      - shared
  - name: shared
contexts:
  - name: orders
    may_import:
      - shared
  - name: shared
packages:
  - pattern: github.com/acme/service/domain/...
    layer: domain
    context: orders
    blocking_functions:
      - Repository.Load
    deny_imports:
      - github.com/acme/service/infrastructure/...
rules:
  architecture/import-boundary:
    status: blocking
    promotion:
      version: 0.1.0
      evidence: ARCH-101 reviewed corpus
`)

	config, err := shared.LoadConfig(path, []string{"architecture/import-boundary"})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.Root != directory {
		t.Fatalf("Root = %q, want %q", config.Root, directory)
	}
	if got := config.Packages[0].DenyImports[0]; got != "github.com/acme/service/infrastructure/..." {
		t.Fatalf("DenyImports[0] = %q", got)
	}
	if config.Packages[0].BlockingFunctions[0] != "Repository.Load" ||
		config.Packages[0].Layer != "domain" ||
		config.Packages[0].Context != "orders" ||
		len(config.Layers) != 2 || config.Layers[0].MayImport[0] != "shared" ||
		len(config.Contexts) != 2 || config.Contexts[0].Name != "orders" ||
		len(config.InitPackages) != 1 || len(config.ContextOwners) != 1 ||
		len(config.HTTPTimeoutExceptions) != 1 ||
		config.HTTPTimeoutExceptions[0] !=
			"github.com/acme/service/internal/streaming" ||
		len(config.Constructors) != 1 ||
		config.Constructors[0].Symbols[0] != "New" ||
		len(config.ResourceConstructors) != 1 ||
		config.ResourceConstructors[0].CleanupResult != 1 ||
		config.ResourceConstructors[0].AllowedPackages[0] !=
			"github.com/acme/service/internal/runtime" ||
		len(config.Transactions) != 1 ||
		config.Transactions[0].Symbol != "DB.BeginTx" ||
		config.Transactions[0].RollbackMethod != "Rollback" ||
		len(config.LockSensitiveCalls) != 1 ||
		config.LockSensitiveCalls[0].Symbol != "Client.Call" ||
		len(config.SensitiveTypes) != 1 ||
		config.SensitiveTypes[0].Name != "Token" ||
		len(config.SensitiveSinks) != 1 ||
		config.SensitiveSinks[0].VariadicFrom == nil ||
		*config.SensitiveSinks[0].VariadicFrom != 3 ||
		len(config.ForbiddenAPIs) != 1 ||
		config.ForbiddenAPIs[0].Symbol != "Client.Call" ||
		len(config.BackendClients) != 1 ||
		config.BackendClients[0].AllowedPackages[0] !=
			"github.com/acme/service/internal/adapter/..." ||
		len(config.MutableGlobals) != 1 ||
		config.MutableGlobals[0].Package !=
			"github.com/acme/service/internal/runtime/..." ||
		len(config.InterfaceProviderPackages) != 1 ||
		config.InterfaceProviderPackages[0] !=
			"github.com/acme/service/internal/provider/..." ||
		len(config.InterfaceNames) != 1 ||
		config.InterfaceNames[0].RequiredPrefix != "Order" ||
		config.InterfaceNames[0].RequiredSuffix != "Port" ||
		config.InterfaceNames[0].AllowedNames[0] != "Compatibility" ||
		len(config.MetricLabelTypes) != 1 ||
		config.MetricLabelTypes[0].Name != "UserID" ||
		len(config.MetricLabelSinks) != 1 ||
		config.MetricLabelSinks[0].Arguments[0] != 0 ||
		len(config.MetricLabelNameTypes) != 1 ||
		config.MetricLabelNameTypes[0].Name != "MetricName" ||
		len(config.MetricLabelNameSinks) != 1 ||
		config.MetricLabelNameSinks[0].Arguments[0] != 0 ||
		len(config.BackendErrorBoundaries) != 1 ||
		len(config.BackendErrorSources) != 1 ||
		config.BackendErrorSources[0].Result != 1 ||
		len(config.BackendErrorPassthroughs) != 1 ||
		*config.BackendErrorPassthroughs[0].VariadicFrom != 1 ||
		len(config.GoroutineFanout) != 1 ||
		config.GoroutineFanout[0].MaxStatic != 8 ||
		!config.Generated.Exclude || len(config.Generated.Paths) != 1 ||
		config.Generated.Paths[0] != "generated/client.go" ||
		config.Rules["architecture/import-boundary"].Promotion == nil ||
		config.Rules["architecture/import-boundary"].Promotion.Version != "0.1.0" ||
		config.Rules["architecture/import-boundary"].Promotion.Evidence !=
			"ARCH-101 reviewed corpus" {
		t.Fatalf("extended policy = %#v", config)
	}
}

func TestLoadConfigRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"unknown top-level key":   "version: 1\nstyle: strict\n",
		"unsupported inheritance": "version: 1\nextends: parent.yml\n",
		"unsupported version":     "version: 2\n",
		"unknown rule": "version: 1\nrules:\n" +
			"  security/unknown:\n    status: blocking\n",
		"blocking without promotion": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    status: blocking\n",
		"invalid rule status": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    status: mandatory\n",
		"invalid rule severity": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    severity: fatal\n",
		"blocking with invalid promotion version": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    status: blocking\n    promotion:\n" +
			"      version: next\n      evidence: SEC-1\n",
		"blocking with empty promotion evidence": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    status: blocking\n    promotion:\n" +
			"      version: 0.1.0\n      evidence: '  '\n",
		"advisory with promotion evidence": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    status: advisory\n    promotion:\n" +
			"      version: 0.1.0\n      evidence: SEC-1\n",
		"unsupported rule options": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    options:\n      mode: strict\n",
		"empty rule options": "version: 1\nrules:\n" +
			"  architecture/import-boundary:\n    options: {}\n",
		"unsupported adapter roots": "version: 1\nadapter_roots:\n" +
			"  - github.com/acme/service/internal/adapters/...\n",
		"empty adapter roots": "version: 1\nadapter_roots: []\n",
		"generated exclusion without exact paths": "version: 1\ngenerated:\n" +
			"  exclude: true\n",
		"generated paths without exclusion": "version: 1\ngenerated:\n" +
			"  paths: [generated/client.go]\n",
		"absolute generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: [/generated/client.go]\n",
		"drive generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: ['C:/generated/client.go']\n",
		"traversing generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: [../generated/client.go]\n",
		"backslash generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: ['generated\\client.go']\n",
		"wildcard generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: ['generated/*.go']\n",
		"ellipsis generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: [generated/.../client.go]\n",
		"non-Go generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: [generated/client.txt]\n",
		"duplicate generated path": "version: 1\ngenerated:\n  exclude: true\n" +
			"  paths: [generated/client.go, generated/client.go]\n",
		"unknown exception rule": "version: 1\nexceptions:\n" +
			"  - rule: security/unknown\n    package: example.com/p\n" +
			"    reason: reviewed\n",
		"empty exception package": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n    package: '  '\n" +
			"    reason: reviewed\n",
		"empty exception reason": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service/legacy\n" +
			"    reason: '  '\n",
		"wildcard exception package": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service/...\n    reason: reviewed\n",
		"absolute exception package": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: /service\n    reason: reviewed\n",
		"dirty exception package": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/../service\n    reason: reviewed\n",
		"traversing exception path": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: ../service.go\n" +
			"    reason: reviewed\n",
		"absolute exception path": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: /service.go\n" +
			"    reason: reviewed\n",
		"drive exception path": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: 'C:/service.go'\n" +
			"    reason: reviewed\n",
		"backslash exception path": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: internal\\service.go\n" +
			"    reason: reviewed\n",
		"invalid exception expiry": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    reason: reviewed\n" +
			"    expires: tomorrow\n",
		"empty exception issue": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    reason: reviewed\n" +
			"    issue: '  '\n",
		"duplicate exception": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: service.go\n" +
			"    reason: first\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: service.go\n" +
			"    reason: second\n",
		"overlapping exception": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    reason: package exception\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: service.go\n" +
			"    reason: file exception\n",
		"reverse overlapping exception": "version: 1\nexceptions:\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n    path: service.go\n" +
			"    reason: file exception\n" +
			"  - rule: architecture/import-boundary\n" +
			"    package: github.com/acme/service\n" +
			"    reason: package exception\n",
		"second document":           "version: 1\n---\nversion: 1\n",
		"malformed second document": "version: 1\n---\ninvalid: [\n",
	}

	for name, contents := range tests {
		contents := contents
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeConfig(t, t.TempDir(), contents)
			_, err := shared.LoadConfig(
				path,
				[]string{"architecture/import-boundary"},
			)
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want strict rejection")
			}
		})
	}
}

func TestLoadConfigRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := shared.LoadConfig(
		filepath.Join(t.TempDir(), "missing.yml"),
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "read configuration") {
		t.Fatalf("LoadConfig() error = %v, want read error", err)
	}
}

func TestLoadConfigRejectsOversizedPolicy(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "analysis.yml")
	contents := bytes.Repeat([]byte{'x'}, (1<<20)+1)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := shared.LoadConfig(path, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1048576 bytes") {
		t.Fatalf("LoadConfig() error = %v, want size rejection", err)
	}
}

func TestLoadConfigRejectsUnreadablePolicyContent(t *testing.T) {
	t.Parallel()

	_, err := shared.LoadConfig(t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "read configuration") {
		t.Fatalf("LoadConfig(directory) error = %v, want read error", err)
	}
}

func TestLoadConfigPreservesTrailingDecodeFailure(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, t.TempDir(), "version: 1\n---\ninvalid: [\n")
	_, err := shared.LoadConfig(path, nil)
	if err == nil || !strings.Contains(err.Error(), "decode trailing configuration") {
		t.Fatalf("LoadConfig() error = %v, want trailing decode failure", err)
	}
}

func writeConfig(t *testing.T, directory, contents string) string {
	t.Helper()

	path := filepath.Join(directory, "analysis.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}
