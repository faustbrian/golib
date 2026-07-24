package analysis_test

import (
	"io"
	"strconv"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func BenchmarkReportWriters(b *testing.B) {
	report := benchmarkReport(1_000)
	benchmarks := []struct {
		name  string
		write func() error
	}{
		{name: "JSON", write: func() error { return shared.WriteJSON(io.Discard, report) }},
		{name: "SARIF", write: func() error { return shared.WriteSARIF(io.Discard, report) }},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := benchmark.write(); err != nil {
					b.Fatalf("write report: %v", err)
				}
			}
		})
	}
}

func benchmarkReport(size int) shared.Report {
	rules := []shared.Rule{
		{
			ID:                "context/no-background",
			Category:          shared.CategoryContext,
			Severity:          shared.SeverityWarning,
			DefaultStatus:     shared.StatusAdvisory,
			Rationale:         "Detached contexts hide cancellation ownership.",
			Remediation:       "Accept or derive an owned context.",
			IntroducedVersion: "0.1.0",
		},
		{
			ID:                "security/no-unsafe",
			Category:          shared.CategorySecurity,
			Severity:          shared.SeverityError,
			DefaultStatus:     shared.StatusAdvisory,
			Rationale:         "Unsafe bypasses language guarantees.",
			Remediation:       "Use a safe API.",
			IntroducedVersion: "0.1.0",
		},
	}
	diagnostics := make([]shared.Diagnostic, size)
	for index := range diagnostics {
		rule := rules[index%len(rules)]
		diagnostics[index] = shared.Diagnostic{
			Rule:        rule.ID,
			Package:     "example.com/service/internal/package" + strconv.Itoa(index%32),
			Filename:    "internal/package" + strconv.Itoa(index%32) + "/file.go",
			Line:        index + 1,
			Column:      2,
			Message:     "representative governed diagnostic",
			Rationale:   rule.Rationale,
			Remediation: rule.Remediation,
		}
	}
	return shared.Report{
		ToolVersion:  "0.1.0",
		Rules:        rules,
		Diagnostics:  diagnostics,
		Exceptions:   []shared.PolicyException{},
		Suppressions: []shared.Suppression{},
	}
}
