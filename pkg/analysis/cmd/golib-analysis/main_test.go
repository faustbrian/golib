package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"github.com/faustbrian/golib/pkg/analysis/policy"
	toolanalysis "golang.org/x/tools/go/analysis"
)

func TestRunCheckEmitsJSONAndPreservesBlockingStatus(t *testing.T) {
	t.Parallel()

	root, config := writeCheckModule(t, "blocking")
	var output bytes.Buffer
	handled, blocking, err := runCheck(context.Background(), []string{
		"check", "-config", config, "-format", "json", "./...",
	}, &output)
	if err != nil || !handled || !blocking {
		t.Fatalf("runCheck() = %t, %t, %v", handled, blocking, err)
	}
	var report shared.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("report JSON error = %v", err)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Filename != "sample/sample.go" {
		t.Fatalf("report = %#v (root %s)", report, root)
	}
}

func TestRunCheckEmitsSARIFForAdvisoryFinding(t *testing.T) {
	t.Parallel()

	_, config := writeCheckModule(t, "advisory")
	var output bytes.Buffer
	handled, blocking, err := runCheck(context.Background(), []string{
		"check", "-config", config, "-format", "sarif", "-sequential",
	}, &output)
	if err != nil || !handled || blocking {
		t.Fatalf("runCheck() = %t, %t, %v", handled, blocking, err)
	}
	var document map[string]any
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("SARIF JSON error = %v", err)
	}
	if document["version"] != "2.1.0" {
		t.Fatalf("SARIF version = %#v", document["version"])
	}
}

func TestRunCheckSupportsCanonicalPolicyOutsideTargetRoot(t *testing.T) {
	t.Parallel()

	root, _ := writeCheckModule(t, "advisory")
	canonical := filepath.Join(t.TempDir(), "service.yml")
	contents := []byte("version: 1\nrules:\n" +
		"  security/no-unsafe:\n    status: advisory\n")
	if err := os.WriteFile(canonical, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var output bytes.Buffer
	handled, blocking, err := runCheck(context.Background(), []string{
		"check", "-config", canonical, "-root", root, "./...",
	}, &output)
	if err != nil || !handled || blocking {
		t.Fatalf("runCheck() = %t, %t, %v", handled, blocking, err)
	}
	var report shared.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("report JSON error = %v", err)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Filename != "sample/sample.go" {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunCheckRejectsInvalidArgumentsAndOutput(t *testing.T) {
	t.Parallel()

	if handled, _, err := runCheck(context.Background(), []string{"rules"}, &bytes.Buffer{}); handled || err != nil {
		t.Fatalf("runCheck(rules) = %t, %v", handled, err)
	}
	tests := [][]string{
		{"check"},
		{"check", "-unknown"},
		{"check", "-config", "missing.yml"},
		{"check", "-config", "missing.yml", "-format", "yaml"},
	}
	for _, arguments := range tests {
		if handled, _, err := runCheck(context.Background(), arguments, &bytes.Buffer{}); !handled || err == nil {
			t.Fatalf("runCheck(%q) = %t, %v", arguments, handled, err)
		}
	}
	_, config := writeCheckModule(t, "advisory")
	if handled, _, err := runCheck(context.Background(), []string{
		"check", "-config", config, "-root", "relative",
	}, &bytes.Buffer{}); !handled || err == nil {
		t.Fatalf("runCheck(relative root) = %t, %v", handled, err)
	}
	if handled, _, err := runCheck(context.Background(), []string{
		"check", "-config", config,
	}, failingWriter{}); !handled || err == nil {
		t.Fatalf("runCheck(writer) = %t, %v", handled, err)
	}
}

func TestCommandEntrypointMapsOutcomesAndFallsBackToVettool(t *testing.T) {
	t.Run("main wrapper", func(t *testing.T) {
		original := os.Args
		os.Args = []string{"golib-analysis", "version"}
		t.Cleanup(func() { os.Args = original })
		main()
	})

	_, blockingConfig := writeCheckModule(t, "blocking")
	_, advisoryConfig := writeCheckModule(t, "advisory")
	tests := []struct {
		name          string
		arguments     []string
		wantExit      int
		wantFallback  bool
		wantErrorText bool
	}{
		{name: "check error", arguments: []string{"check"}, wantExit: 2, wantErrorText: true},
		{name: "blocking check", arguments: []string{"check", "-config", blockingConfig}, wantExit: 1},
		{name: "advisory check", arguments: []string{"check", "-config", advisoryConfig}},
		{name: "utility error", arguments: []string{"version", "extra"}, wantExit: 2, wantErrorText: true},
		{name: "utility success", arguments: []string{"version"}},
		{name: "vettool fallback", arguments: []string{"./..."}, wantFallback: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var errorOutput bytes.Buffer
			exitCode := 0
			fallbackCalls := 0
			runCommand(
				test.arguments,
				&output,
				&errorOutput,
				func(code int) { exitCode = code },
				func(configured ...*toolanalysis.Analyzer) {
					fallbackCalls++
					if len(configured) != len(analyzers()) {
						t.Fatalf("fallback analyzer count = %d", len(configured))
					}
				},
			)
			if exitCode != test.wantExit {
				t.Fatalf("exit code = %d, want %d", exitCode, test.wantExit)
			}
			if got := fallbackCalls == 1; got != test.wantFallback {
				t.Fatalf("fallback called = %t, want %t", got, test.wantFallback)
			}
			if got := errorOutput.Len() > 0; got != test.wantErrorText {
				t.Fatalf("error output present = %t, want %t", got, test.wantErrorText)
			}
		})
	}

	t.Run("stderr failure", func(t *testing.T) {
		exitCalls := 0
		fallbackCalls := 0
		runCommand(
			[]string{"check"},
			&bytes.Buffer{},
			failingWriter{},
			func(code int) {
				exitCalls++
				if code != 2 {
					t.Fatalf("exit code = %d, want 2", code)
				}
			},
			func(...*toolanalysis.Analyzer) { fallbackCalls++ },
		)
		if exitCalls != 1 || fallbackCalls != 0 {
			t.Fatalf("exit calls = %d, fallback calls = %d", exitCalls, fallbackCalls)
		}
	})
}

func TestAnalyzersAreDeterministic(t *testing.T) {
	t.Parallel()

	configured := analyzers()
	names := make([]string, 0, len(configured))
	for _, analyzer := range configured {
		names = append(names, analyzer.Name)
	}
	want := []string{
		"backenderror",
		"blockingcontext",
		"cleanupownership",
		"transactionrollback",
		"constructorgoroutine",
		"forbiddenapi",
		"globalgoroutine",
		"goroutinefanout",
		"httpclienttimeout",
		"importboundary",
		"lockacrosscall",
		"nobackground",
		"nodefaulthttp",
		"noinit",
		"noprocesscontrol",
		"nostoredcontext",
		"sensitivesink",
		"nounsafe",
		"mutableglobal",
		"interfacenaming",
		"interfaceplacement",
		"metriccardinality",
		"metriclabelname",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("analyzer names = %#v, want %#v", names, want)
	}
}

func TestRunUtilityPrintsRuleInventory(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	handled, err := runUtility([]string{"rules"}, &output)
	if err != nil || !handled {
		t.Fatalf("runUtility() = %t, %v", handled, err)
	}
	var entries []policy.Entry
	if err := json.Unmarshal(output.Bytes(), &entries); err != nil {
		t.Fatalf("inventory JSON error = %v", err)
	}
	if len(entries) != 23 {
		t.Fatalf("len(entries) = %d, want 23", len(entries))
	}
}

func TestRunUtilityPrintsVersion(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	handled, err := runUtility([]string{"version"}, &output)
	if err != nil || !handled || output.String() != "0.1.0\n" {
		t.Fatalf("runUtility(version) = %t, %q, %v", handled, output.String(), err)
	}
}

func TestRunUtilityValidatesConfiguration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "analysis.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var output bytes.Buffer
	handled, err := runUtility([]string{"validate-config", path}, &output)
	if err != nil || !handled || output.String() != "configuration valid\n" {
		t.Fatalf("runUtility() = %t, %q, %v", handled, output.String(), err)
	}
}

func TestRunUtilityRejectsInvalidAnalyzerConfiguration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "analysis.yml")
	contents := []byte(`version: 1
forbidden_apis:
  - package: example.com/legacy/*
    symbol: Old
    replacement: example.com/modern.New
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if handled, err := runUtility(
		[]string{"validate-config", path},
		&bytes.Buffer{},
	); !handled || err == nil {
		t.Fatalf("runUtility() = %t, %v", handled, err)
	}
}

func TestRunUtilityChecksAndUpdatesCanonicalPolicy(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	canonical := filepath.Join(directory, "canonical.yml")
	local := filepath.Join(directory, "analysis.yml")
	contents := []byte("version: 1\n")
	if err := os.WriteFile(canonical, contents, 0o600); err != nil {
		t.Fatalf("WriteFile(canonical) error = %v", err)
	}
	if err := os.WriteFile(local, []byte("version: 1\nrules: {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(local) error = %v", err)
	}

	if handled, err := runUtility(
		[]string{"sync-policy", "check", canonical, local},
		&bytes.Buffer{},
	); !handled || err == nil || err.Error() != "policy drift: local policy differs from canonical policy" {
		t.Fatalf("runUtility(check drift) = %t, %v", handled, err)
	}
	var output bytes.Buffer
	if handled, err := runUtility(
		[]string{"sync-policy", "update", canonical, local},
		&output,
	); !handled || err != nil || output.String() != "policy synchronized\n" {
		t.Fatalf("runUtility(update) = %t, %q, %v", handled, output.String(), err)
	}
	got, err := os.ReadFile(local) // #nosec G304 -- test-owned temporary path
	if err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("ReadFile(local) = %q, %v", got, err)
	}
	output.Reset()
	if handled, err := runUtility(
		[]string{"sync-policy", "check", canonical, local},
		&output,
	); !handled || err != nil || output.String() != "policy in sync\n" {
		t.Fatalf("runUtility(check) = %t, %q, %v", handled, output.String(), err)
	}
}

func TestRunUtilityRejectsInvalidPolicySync(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	invalid := filepath.Join(directory, "invalid.yml")
	local := filepath.Join(directory, "analysis.yml")
	if err := os.WriteFile(invalid, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}
	if err := os.WriteFile(local, []byte("preserve me\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(local) error = %v", err)
	}

	tests := [][]string{
		{"sync-policy"},
		{"sync-policy", "copy", invalid, local},
		{"sync-policy", "update", filepath.Join(directory, "missing.yml"), local},
		{"sync-policy", "update", invalid, local},
		{"sync-policy", "check", invalid, local},
	}
	for _, arguments := range tests {
		if handled, err := runUtility(arguments, &bytes.Buffer{}); !handled || err == nil {
			t.Fatalf("runUtility(%q) = %t, %v", arguments, handled, err)
		}
	}
	got, err := os.ReadFile(local) // #nosec G304 -- test-owned temporary path
	if err != nil || string(got) != "preserve me\n" {
		t.Fatalf("local policy = %q, %v", got, err)
	}
}

func TestRunPolicySyncPropagatesDependencies(t *testing.T) {
	t.Parallel()

	factory := func() (*policy.Registry, error) { return policy.Builtin() }
	validConfig := func(string, []string) (*shared.Config, error) {
		return &shared.Config{Version: 1}, nil
	}
	tests := []struct {
		name         string
		arguments    []string
		dependencies policySyncDependencies
	}{
		{
			name:      "load",
			arguments: []string{"sync-policy", "check", "canonical", "local"},
			dependencies: policySyncDependencies{
				loadConfig: func(string, []string) (*shared.Config, error) {
					return nil, errors.New("load failure")
				},
			},
		},
		{
			name:      "validate",
			arguments: []string{"sync-policy", "check", "canonical", "local"},
			dependencies: policySyncDependencies{
				loadConfig: validConfig,
				validate: func(*shared.Config) error {
					return errors.New("validation failure")
				},
			},
		},
		{
			name:      "canonical read",
			arguments: []string{"sync-policy", "check", "canonical", "local"},
			dependencies: policySyncDependencies{
				loadConfig: validConfig,
				validate:   func(*shared.Config) error { return nil },
				readFile: func(string) ([]byte, error) {
					return nil, errors.New("read failure")
				},
			},
		},
		{
			name:      "local read",
			arguments: []string{"sync-policy", "check", "canonical", "local"},
			dependencies: policySyncDependencies{
				loadConfig: validConfig,
				validate:   func(*shared.Config) error { return nil },
				readFile: func(path string) ([]byte, error) {
					if path == "canonical" {
						return []byte("version: 1\n"), nil
					}
					return nil, errors.New("read failure")
				},
			},
		},
		{
			name:      "local write",
			arguments: []string{"sync-policy", "update", "canonical", "local"},
			dependencies: policySyncDependencies{
				loadConfig: validConfig,
				validate:   func(*shared.Config) error { return nil },
				readFile: func(string) ([]byte, error) {
					return []byte("version: 1\n"), nil
				},
				writeFile: func(string, []byte, os.FileMode) error {
					return errors.New("write failure")
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handled, err := runPolicySyncWithDependencies(
				test.arguments,
				&bytes.Buffer{},
				factory,
				test.dependencies,
			)
			if !handled || err == nil {
				t.Fatalf("runPolicySyncWithDependencies() = %t, %v", handled, err)
			}
		})
	}
}

func TestRunUtilityRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{"version", "extra"},
		{"rules", "extra"},
		{"validate-config"},
		{"validate-config", filepath.Join(t.TempDir(), "missing.yml")},
	}
	for _, arguments := range tests {
		if handled, err := runUtility(arguments, &bytes.Buffer{}); !handled || err == nil {
			t.Fatalf("runUtility(%q) = %t, %v", arguments, handled, err)
		}
	}
	if handled, err := runUtility([]string{"./..."}, &bytes.Buffer{}); handled || err != nil {
		t.Fatalf("runUtility(package) = %t, %v", handled, err)
	}
	if handled, err := runUtility(nil, &bytes.Buffer{}); handled || err != nil {
		t.Fatalf("runUtility(nil) = %t, %v", handled, err)
	}
}

func TestRunUtilityPropagatesWriterFailures(t *testing.T) {
	t.Parallel()

	if handled, err := runUtility([]string{"version"}, failingWriter{}); !handled || err == nil {
		t.Fatalf("version writer failure = %t, %v", handled, err)
	}
	if handled, err := runUtility([]string{"rules"}, failingWriter{}); !handled || err == nil {
		t.Fatalf("rules writer failure = %t, %v", handled, err)
	}
	path := filepath.Join(t.TempDir(), "analysis.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if handled, err := runUtility(
		[]string{"validate-config", path},
		failingWriter{},
	); !handled || err == nil {
		t.Fatalf("validation writer failure = %t, %v", handled, err)
	}
	for _, mode := range []string{"check", "update"} {
		if handled, err := runUtility(
			[]string{"sync-policy", mode, path, path},
			failingWriter{},
		); !handled || err == nil {
			t.Fatalf("sync-policy %s writer failure = %t, %v", mode, handled, err)
		}
	}
}

func TestRunUtilityPropagatesRegistryFailures(t *testing.T) {
	t.Parallel()

	factory := func() (*policy.Registry, error) {
		return nil, errors.New("invalid built-in policy")
	}
	for _, arguments := range [][]string{
		{"rules"},
		{"validate-config", "policy.yml"},
		{"sync-policy", "check", "canonical.yml", "policy.yml"},
	} {
		handled, err := runUtilityWithRegistry(arguments, &bytes.Buffer{}, factory)
		if !handled || err == nil {
			t.Fatalf("runUtilityWithRegistry(%q) = %t, %v", arguments, handled, err)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, bytes.ErrTooLarge
}

func writeCheckModule(t *testing.T, status string) (string, string) {
	t.Helper()

	promotion := ""
	if status == "blocking" {
		promotion = "    promotion:\n" +
			"      version: 0.1.0\n" +
			"      evidence: command integration fixture\n"
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":           "module example.com/check\n\ngo 1.26.0\n",
		"sample/sample.go": "package sample\nimport _ \"unsafe\"\n",
		"analysis.yml": "version: 1\nrules:\n" +
			"  security/no-unsafe:\n    status: " + status + "\n" + promotion,
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	return root, filepath.Join(root, "analysis.yml")
}
