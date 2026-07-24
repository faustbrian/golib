package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestRequiredDocumentationSetIsPresentAndSubstantive(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"README.md": {
			"go get github.com/faustbrian/golib/pkg/cli",
			"Why explicit commands?",
			"ECS",
		},
		"docs/architecture.md":        {"Lifecycle", "errors.Join", "middleware"},
		"docs/commands.md":            {"ValueExplicit", "TypedOption", "Secret"},
		"docs/parsing.md":             {"--name=value", "Invalid UTF-8", "Windows"},
		"docs/output.md":              {"go-cli/v1", "short write", "stderr"},
		"docs/errors-and-shutdown.md": {"130", "ShutdownController", "signal.Stop"},
		"docs/generation.md":          {"PowerShell", "__complete", "never edits"},
		"docs/operations.md":          {"ECS one-off task", "CI job", "Backfill"},
		"docs/integrations.md":        {"config", "prompts", "telemetry"},
		"docs/migrations.md":          {"Cobra", "Symfony Console", "Laravel Artisan"},
		"docs/security.md":            {"process", "terminal controls", "unsafe"},
		"docs/performance.md":         {"urfave/cli", "Kong", "standard `flag`"},
		"docs/mutation.md":            {"100% efficacy", "no survivors", "release blocker"},
		"docs/compatibility.md":       {"Semantic Versioning", "GOWORK=off", "SBOM"},
		"docs/troubleshooting.md":     {"FAQ", "NonInteractive", "global registry"},
		"docs/limitations.md":         {"prompts", "Cobra", "does not"},
		"docs/audit/2026-07-22-hardening.md": {
			"Command graph conformance",
			"Lifecycle and failure matrix",
			"674 killed",
			"Findings registry",
			"Release gate inventory",
		},
		"SECURITY.md":  {"Security Advisories", "os.Exit", "signal"},
		"CHANGELOG.md": {"Unreleased"},
		"LICENSE":      {"MIT License"},
	}
	for path, phrases := range required {
		path := path
		phrases := phrases
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			// #nosec G304 -- path comes from the fixed required-document map.
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if len(contents) < 100 {
				t.Fatalf("%s is not substantive", path)
			}
			for _, phrase := range phrases {
				if !strings.Contains(string(contents), phrase) {
					t.Errorf("%s does not contain %q", path, phrase)
				}
			}
		})
	}
}
