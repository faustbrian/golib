package analysis_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func TestWriteJSONIsDeterministicWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	report := shared.Report{
		ToolVersion: "0.1.0",
		Rules: []shared.Rule{
			{ID: "security/no-unsafe"},
			{ID: "context/no-background"},
		},
		Diagnostics: []shared.Diagnostic{
			{Rule: "security/no-unsafe", Filename: "z.go", Line: 9, Message: "unsafe"},
			{Rule: "context/no-background", Filename: "a.go", Line: 2, Message: "context"},
		},
		Exceptions: []shared.PolicyException{
			{Rule: "security/no-unsafe", Package: "z/package", Path: "z.go"},
			{Rule: "context/no-background", Package: "a/package", Path: "a.go"},
		},
		Suppressions: []shared.Suppression{
			{Rule: "security/no-unsafe", Filename: "z.go", DirectiveLine: 8},
			{Rule: "context/no-background", Filename: "a.go", DirectiveLine: 1},
		},
	}
	originalFirst := report.Diagnostics[0]
	originalException := report.Exceptions[0]
	var first bytes.Buffer
	if err := shared.WriteJSON(&first, report); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	report.Diagnostics[0], report.Diagnostics[1] = report.Diagnostics[1], report.Diagnostics[0]
	report.Exceptions[0], report.Exceptions[1] = report.Exceptions[1], report.Exceptions[0]
	var second bytes.Buffer
	if err := shared.WriteJSON(&second, report); err != nil {
		t.Fatalf("WriteJSON() second error = %v", err)
	}
	if first.String() != second.String() {
		t.Fatalf("JSON output is order-dependent:\n%s\n%s", first.String(), second.String())
	}
	var normalized shared.Report
	if err := json.Unmarshal(first.Bytes(), &normalized); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if normalized.Rules[0].ID != "context/no-background" ||
		normalized.Diagnostics[0].Filename != "a.go" ||
		normalized.Exceptions[0].Rule != "context/no-background" ||
		normalized.Suppressions[0].Filename != "a.go" {
		t.Fatalf("normalized order = %#v", normalized)
	}
	if originalFirst != report.Diagnostics[1] {
		t.Fatal("WriteJSON() mutated caller diagnostics")
	}
	if originalException != report.Exceptions[1] {
		t.Fatal("WriteJSON() mutated caller exceptions")
	}
}

func TestWriteJSONRelativizesRepositoryPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	report := shared.Report{
		Root: root,
		Diagnostics: []shared.Diagnostic{{
			Filename: filepath.Join(root, "internal", "service.go"),
		}},
		Suppressions: []shared.Suppression{
			{Filename: filepath.Join(root, "z.go"), DirectiveLine: 2},
			{Filename: filepath.Join(root, "a.go"), DirectiveLine: 1},
		},
	}
	var output bytes.Buffer
	if err := shared.WriteJSON(&output, report); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	var got shared.Report
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Diagnostics[0].Filename != "internal/service.go" ||
		got.Suppressions[0].Filename != "a.go" {
		t.Fatalf("normalized report = %#v", got)
	}
}

func TestWritersEncodeEmptyInventoriesAsArrays(t *testing.T) {
	t.Parallel()

	var jsonOutput bytes.Buffer
	if err := shared.WriteJSON(&jsonOutput, shared.Report{}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	for _, field := range []string{"rules", "diagnostics", "exceptions", "suppressions"} {
		if !strings.Contains(jsonOutput.String(), `"`+field+`":[]`) {
			t.Errorf("JSON %s is not an empty array: %s", field, jsonOutput.String())
		}
	}

	var sarifOutput bytes.Buffer
	if err := shared.WriteSARIF(&sarifOutput, shared.Report{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}
	for _, field := range []string{"rules", "results", "exceptions", "suppressions"} {
		if !strings.Contains(sarifOutput.String(), `"`+field+`":[]`) {
			t.Errorf("SARIF %s is not an empty array: %s", field, sarifOutput.String())
		}
	}
}

func TestWritersRejectPathsThatCouldExposeWorkspace(t *testing.T) {
	t.Parallel()

	absolute := filepath.Join(t.TempDir(), "secret.go")
	reports := []shared.Report{
		{Diagnostics: []shared.Diagnostic{{Filename: absolute}}},
		{Suppressions: []shared.Suppression{{Filename: absolute}}},
		{Diagnostics: []shared.Diagnostic{{Filename: "../secret.go"}}},
		{Suppressions: []shared.Suppression{{Filename: "../secret.go"}}},
		{
			Root:        t.TempDir(),
			Diagnostics: []shared.Diagnostic{{Filename: absolute}},
		},
	}
	for _, report := range reports {
		var output bytes.Buffer
		if err := shared.WriteJSON(&output, report); err == nil {
			t.Fatal("WriteJSON() accepted unsafe absolute path")
		}
		if err := shared.WriteSARIF(&output, report); err == nil {
			t.Fatal("WriteSARIF() accepted unsafe absolute path")
		}
	}
}

func TestWriteSARIFIncludesStableRulesAndNoSource(t *testing.T) {
	t.Parallel()

	report := shared.Report{
		ToolVersion: "0.1.0",
		Rules: []shared.Rule{
			{
				ID:                "security/no-unsafe",
				Category:          shared.CategorySecurity,
				Severity:          shared.SeverityError,
				DefaultStatus:     shared.StatusAdvisory,
				Rationale:         "Unsafe bypasses language guarantees.",
				Remediation:       "Use a safe API.",
				IntroducedVersion: "0.1.0",
			},
			{ID: "context/no-background", Severity: shared.SeverityWarning},
			{ID: "api/broad-interface", Severity: shared.SeverityInfo},
		},
		Diagnostics: []shared.Diagnostic{
			{Rule: "security/no-unsafe", Filename: "internal/unsafe.go", Line: 4, Column: 2, Message: "unsafe import"},
			{Rule: "context/no-background", Filename: "context.go", Line: 2, Message: "context"},
			{Rule: "api/broad-interface", Filename: "api.go", Line: 3, Message: "interface"},
			{Rule: "unknown/rule", Filename: "unknown.go", Line: 1, Message: "unknown"},
		},
		Exceptions: []shared.PolicyException{{
			Rule: "security/no-unsafe", Package: "example.com/service", Used: true,
		}},
		Suppressions: []shared.Suppression{{
			Rule: "context/no-background", Filename: "context.go", Used: true,
		}},
	}
	var output bytes.Buffer
	if err := shared.WriteSARIF(&output, report); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}
	if strings.Contains(output.String(), "source") || strings.Contains(output.String(), "snippet") {
		t.Fatalf("SARIF unexpectedly embeds source: %s", output.String())
	}
	var document map[string]any
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("SARIF JSON error = %v", err)
	}
	if document["version"] != "2.1.0" {
		t.Fatalf("SARIF version = %#v", document["version"])
	}
	runs := document["runs"].([]any)
	properties := runs[0].(map[string]any)["properties"].(map[string]any)
	if len(properties["exceptions"].([]any)) != 1 ||
		len(properties["suppressions"].([]any)) != 1 {
		t.Fatalf("SARIF inventory properties = %#v", properties)
	}
	results := runs[0].(map[string]any)["results"].([]any)
	foundUnknown := false
	for _, raw := range results {
		result := raw.(map[string]any)
		if result["ruleId"] != "unknown/rule" {
			continue
		}
		foundUnknown = true
		if result["level"] != "warning" {
			t.Fatalf("unknown rule level = %#v, want warning", result["level"])
		}
	}
	if !foundUnknown {
		t.Fatal("SARIF results omitted unknown rule")
	}
}

func TestWritersPropagateOutputFailure(t *testing.T) {
	t.Parallel()

	writer := failingWriter{}
	if err := shared.WriteJSON(writer, shared.Report{}); err == nil {
		t.Fatal("WriteJSON() error = nil")
	}
	if err := shared.WriteSARIF(writer, shared.Report{}); err == nil {
		t.Fatal("WriteSARIF() error = nil")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, bytes.ErrTooLarge
}
