package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOperationalAssurance(t *testing.T) {
	t.Parallel()

	requiredScenarios := []string{
		"OA-REFERENCE-HTTP",
		"OA-REFERENCE-DURABILITY",
		"OA-REFERENCE-EXTERNAL",
		"OA-PLATFORM-MATRIX",
		"OA-FAILURE-RECOVERY",
		"OA-DEPLOYMENT-COMPATIBILITY",
		"OA-RESOURCE-PERFORMANCE",
		"OA-SECURITY-PRIVACY-SUPPLY-CHAIN",
		"OA-OBSERVABILITY-OPERATIONS",
		"OA-CROSS-PACKAGE-CONSISTENCY",
		"OA-RELEASE-CONSUMER",
	}
	newRecord := func() map[string]any {
		scenarios := make([]map[string]any, 0, len(requiredScenarios))
		for _, identifier := range requiredScenarios {
			scenarios = append(scenarios, map[string]any{
				"id":               identifier,
				"status":           "pending",
				"affected_modules": []string{"*"},
				"evidence":         []any{},
				"accepted_risks":   []any{},
			})
		}
		return map[string]any{
			"schema_version":          1,
			"verdict":                 "not ready",
			"modules":                 []string{"pkg/a", "pkg/b"},
			"scenarios":               scenarios,
			"residual_risks":          []string{"GL-RISK-001"},
			"risk_acceptances":        []any{},
			"input_digest_migrations": []any{},
		}
	}
	current := catalog{Modules: []module{
		{
			Directory:           "pkg/a",
			Path:                "example.test/a",
			Releasable:          true,
			ReverseDependencies: []string{"example.test/b"},
		},
		{Directory: "pkg/b", Path: "example.test/b", Releasable: true},
		{Directory: "pkg/fixture", Releasable: false},
	}}

	tests := []struct {
		name       string
		mutate     func(*testing.T, string, map[string]any)
		suffix     string
		wantSubstr string
	}{
		{name: "complete pending register"},
		{
			name: "pending scenario retains historical evidence",
			mutate: func(t *testing.T, root string, record map[string]any) {
				artifact := filepath.Join(root, "historical-evidence.txt")
				contents := []byte("historical evidence\n")
				if err := os.WriteFile(artifact, contents, 0o600); err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(contents)
				scenarios := record["scenarios"].([]map[string]any)
				scenarios[0]["evidence"] = []map[string]any{{
					"path":         "historical-evidence.txt",
					"sha256":       hex.EncodeToString(digest[:]),
					"observed_utc": "2026-08-12T00:00:00Z",
					"environment":  "historical test",
					"module_scope": []string{"*"},
					"input_digests": map[string]string{
						"pkg/a": strings.Repeat("0", 64),
						"pkg/b": strings.Repeat("1", 64),
					},
				}}
			},
		},
		{
			name: "missing releasable module",
			mutate: func(_ *testing.T, _ string, record map[string]any) {
				record["modules"] = []string{"pkg/a"}
			},
			wantSubstr: "module scope",
		},
		{
			name: "missing scenario",
			mutate: func(_ *testing.T, _ string, record map[string]any) {
				scenarios := record["scenarios"].([]map[string]any)
				record["scenarios"] = scenarios[1:]
			},
			wantSubstr: "required scenario",
		},
		{
			name: "duplicate scenario",
			mutate: func(_ *testing.T, _ string, record map[string]any) {
				scenarios := record["scenarios"].([]map[string]any)
				record["scenarios"] = append(scenarios, scenarios[0])
			},
			wantSubstr: "duplicate scenario",
		},
		{
			name:       "trailing JSON value",
			suffix:     `{}`,
			wantSubstr: "trailing data",
		},
		{
			name: "ready with pending scenario",
			mutate: func(_ *testing.T, _ string, record map[string]any) {
				record["verdict"] = "ready"
				record["residual_risks"] = []string{}
			},
			wantSubstr: "requires every scenario to pass",
		},
		{
			name: "ready with residual risk",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				record["verdict"] = "ready"
			},
			wantSubstr: "cannot retain residual risks",
		},
		{
			name: "accepted-risk verdict without risks",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				record["verdict"] = "ready with named accepted risks"
				record["residual_risks"] = []string{}
			},
			wantSubstr: "requires at least one residual risk",
		},
		{
			name: "accepted-risk verdict with matched approval",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
			},
		},
		{
			name: "accepted-risk verdict accepts numeric UTC offset",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
				approvals := record["risk_acceptances"].([]map[string]any)
				approvals[0]["accepted_utc"] = "2026-08-12T00:00:00+00:00"
			},
		},
		{
			name: "approval scenario does not name risk",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
				scenarios := record["scenarios"].([]map[string]any)
				scenarios[0]["status"] = "passed"
				scenarios[0]["accepted_risks"] = []string{}
			},
			wantSubstr: "does not name risk",
		},
		{
			name: "accepted-risk scenario lacks matching approval scope",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
				scenarios := record["scenarios"].([]map[string]any)
				scenarios[1]["status"] = "accepted risk"
				scenarios[1]["accepted_risks"] = []string{"GL-RISK-001"}
			},
			wantSubstr: "does not authorize scenario",
		},
		{
			name: "pending scenario cannot name accepted risk",
			mutate: func(_ *testing.T, _ string, record map[string]any) {
				scenarios := record["scenarios"].([]map[string]any)
				scenarios[0]["accepted_risks"] = []string{"GL-RISK-001"}
			},
			wantSubstr: "accepted risks require accepted-risk status",
		},
		{
			name: "accepted-risk supporting evidence is validated",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				invalid := make(map[string]any, len(evidence[0]))
				for key, value := range evidence[0] {
					invalid[key] = value
				}
				invalid["sha256"] = strings.Repeat("0", 64)
				scenarios[0]["evidence"] = []map[string]any{invalid}
			},
			wantSubstr: "digest mismatch",
		},
		{
			name: "passed evidence digest mismatch",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["sha256"] = strings.Repeat("0", 64)
			},
			wantSubstr: "digest mismatch",
		},
		{
			name: "passed evidence input digest mismatch",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				inputDigests := evidence[0]["input_digests"].(map[string]string)
				inputDigests["pkg/a"] = strings.Repeat("0", 64)
			},
			wantSubstr: "input digest mismatch",
		},
		{
			name: "passed evidence requires a reproducible input environment",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				delete(evidence[0], "input_environment")
			},
			wantSubstr: "input environment is incomplete",
		},
		{
			name: "passed evidence rejects ambiguous input environment values",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				inputEnvironment := evidence[0]["input_environment"].(map[string]string)
				inputEnvironment["kernel"] = "Test\nKernel"
			},
			wantSubstr: "input environment contains control characters",
		},
		{
			name: "passed evidence rejects invalid cgo input environment",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				inputEnvironment := evidence[0]["input_environment"].(map[string]string)
				inputEnvironment["cgo_enabled"] = "enabled"
			},
			wantSubstr: "input environment has invalid cgo_enabled",
		},
		{
			name: "accepted-risk evidence input digest mismatch",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				markScenarioRiskAccepted(record, 0, 0)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				inputDigests := evidence[0]["input_digests"].(map[string]string)
				inputDigests["pkg/a"] = strings.Repeat("0", 64)
			},
			wantSubstr: "input digest mismatch",
		},
		{
			name: "reviewed input digest migration preserves evidence",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				artifact := filepath.Join(root, "digest-migration.md")
				contents := []byte("reviewed non-behavioral digest transition\n")
				if err := os.WriteFile(artifact, contents, 0o600); err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(contents)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_digests"].(map[string]string)["pkg/a"] =
					strings.Repeat("0", 64)
				record["input_digest_migrations"] = []map[string]any{{
					"module":          "pkg/a",
					"from_sha256":     strings.Repeat("0", 64),
					"to_sha256":       strings.Repeat("a", 64),
					"evidence_path":   "digest-migration.md",
					"evidence_sha256": hex.EncodeToString(digest[:]),
					"rationale":       "reviewed non-behavioral digest transition",
				}}
			},
		},
		{
			name: "input digest migration artifact mismatch",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				artifact := filepath.Join(root, "digest-migration.md")
				if err := os.WriteFile(artifact, []byte("reviewed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_digests"].(map[string]string)["pkg/a"] =
					strings.Repeat("0", 64)
				record["input_digest_migrations"] = []map[string]any{{
					"module":          "pkg/a",
					"from_sha256":     strings.Repeat("0", 64),
					"to_sha256":       strings.Repeat("a", 64),
					"evidence_path":   "digest-migration.md",
					"evidence_sha256": strings.Repeat("f", 64),
					"rationale":       "reviewed non-behavioral digest transition",
				}}
			},
			wantSubstr: "migration evidence digest mismatch",
		},
		{
			name: "input digest migration chain preserves evidence",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				evidenceSHA := writeDigestMigrationEvidence(t, root)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_digests"].(map[string]string)["pkg/a"] =
					strings.Repeat("0", 64)
				record["input_digest_migrations"] = []map[string]any{
					newDigestMigration(
						"pkg/a",
						strings.Repeat("0", 64),
						strings.Repeat("1", 64),
						evidenceSHA,
					),
					newDigestMigration(
						"pkg/a",
						strings.Repeat("1", 64),
						strings.Repeat("a", 64),
						evidenceSHA,
					),
				}
			},
		},
		{
			name: "input digest migration does not authorize another digest",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				evidenceSHA := writeDigestMigrationEvidence(t, root)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_digests"].(map[string]string)["pkg/a"] =
					strings.Repeat("2", 64)
				record["input_digest_migrations"] = []map[string]any{
					newDigestMigration(
						"pkg/a",
						strings.Repeat("0", 64),
						strings.Repeat("a", 64),
						evidenceSHA,
					),
				}
			},
			wantSubstr: "input digest mismatch for pkg/a",
		},
		{
			name: "duplicate input digest migration",
			mutate: func(t *testing.T, root string, record map[string]any) {
				evidenceSHA := writeDigestMigrationEvidence(t, root)
				migration := newDigestMigration(
					"pkg/a",
					strings.Repeat("0", 64),
					strings.Repeat("a", 64),
					evidenceSHA,
				)
				record["input_digest_migrations"] = []map[string]any{
					migration,
					migration,
				}
			},
			wantSubstr: "duplicate input digest migration",
		},
		{
			name: "input digest migration rejects unknown module",
			mutate: func(t *testing.T, root string, record map[string]any) {
				evidenceSHA := writeDigestMigrationEvidence(t, root)
				record["input_digest_migrations"] = []map[string]any{
					newDigestMigration(
						"pkg/unknown",
						strings.Repeat("0", 64),
						strings.Repeat("a", 64),
						evidenceSHA,
					),
				}
			},
			wantSubstr: "unknown module pkg/unknown",
		},
		{
			name: "input digest migration supports explicit fixture input",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				evidenceSHA := writeDigestMigrationEvidence(t, root)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_modules"] = []string{"pkg/fixture"}
				evidence[0]["input_digests"].(map[string]string)["pkg/fixture"] =
					strings.Repeat("0", 64)
				record["input_digest_migrations"] = []map[string]any{
					newDigestMigration(
						"pkg/fixture",
						strings.Repeat("0", 64),
						strings.Repeat("c", 64),
						evidenceSHA,
					),
				}
			},
		},
		{
			name: "named module evidence includes reverse dependant input",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				narrowAssuranceScope(record)
				record["verdict"] = "ready"
				record["residual_risks"] = []string{}
			},
		},
		{
			name: "named module evidence omits reverse dependant input",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				narrowAssuranceScope(record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				delete(evidence[0]["input_digests"].(map[string]string), "pkg/b")
			},
			wantSubstr: "input digest scope has 1 modules, want 2",
		},
		{
			name: "evidence binds non-releasable input module",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_modules"] = []string{"pkg/fixture"}
				evidence[0]["input_digests"].(map[string]string)["pkg/fixture"] =
					strings.Repeat("c", 64)
			},
		},
		{
			name: "non-releasable input module requires digest",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_modules"] = []string{"pkg/fixture"}
			},
			wantSubstr: "input digest scope has 2 modules, want 3",
		},
		{
			name: "unknown evidence input module",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["input_modules"] = []string{"pkg/unknown"}
			},
			wantSubstr: "unknown evidence input module pkg/unknown",
		},
		{
			name: "ready with current evidence",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				record["verdict"] = "ready"
				record["residual_risks"] = []string{}
			},
		},
		{
			name: "ready accepts numeric UTC offset",
			mutate: func(t *testing.T, root string, record map[string]any) {
				markScenariosPassed(t, root, record)
				scenarios := record["scenarios"].([]map[string]any)
				evidence := scenarios[0]["evidence"].([]map[string]any)
				evidence[0]["observed_utc"] = "2026-08-12T00:00:00+00:00"
				record["verdict"] = "ready"
				record["residual_risks"] = []string{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			record := newRecord()
			if test.mutate != nil {
				test.mutate(t, root, record)
			}
			contents, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			contents = append(contents, test.suffix...)
			err = validateOperationalAssurance(root, current, contents)
			if test.wantSubstr == "" {
				if err != nil {
					t.Fatalf("validateOperationalAssurance() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantSubstr) {
				t.Fatalf(
					"validateOperationalAssurance() error = %v, want substring %q",
					err,
					test.wantSubstr,
				)
			}
		})
	}
}

func TestOperationalInputEnvironmentReplacesAmbientOverrides(t *testing.T) {
	t.Parallel()

	environment := operationalInputEnvironment{
		GoVersion:  "go1.test",
		GOOS:       "testos",
		GOARCH:     "testarch",
		CGOEnabled: "0",
		Kernel:     "Test Kernel",
		Node:       "missing",
	}
	actual := environment.commandEnvironment([]string{
		"KEEP=value",
		"GOLIB_ASSURANCE_GO_VERSION=ambient",
		"GOLIB_ASSURANCE_KERNEL=ambient",
	})
	want := []string{
		"KEEP=value",
		"GOLIB_ASSURANCE_GO_VERSION=go1.test",
		"GOLIB_ASSURANCE_GOOS=testos",
		"GOLIB_ASSURANCE_GOARCH=testarch",
		"GOLIB_ASSURANCE_CGO_ENABLED=0",
		"GOLIB_ASSURANCE_KERNEL=Test Kernel",
		"GOLIB_ASSURANCE_NODE=missing",
	}
	if strings.Join(actual, "\n") != strings.Join(want, "\n") {
		t.Fatalf("command environment = %v, want %v", actual, want)
	}
}

func writeDigestMigrationEvidence(t *testing.T, root string) string {
	t.Helper()
	contents := []byte("reviewed digest migration\n")
	if err := os.WriteFile(
		filepath.Join(root, "digest-migration.md"),
		contents,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func newDigestMigration(module, from, to, evidenceSHA string) map[string]any {
	return map[string]any{
		"module":          module,
		"from_sha256":     from,
		"to_sha256":       to,
		"evidence_path":   "digest-migration.md",
		"evidence_sha256": evidenceSHA,
		"rationale":       "reviewed non-behavioral digest transition",
	}
}

func narrowAssuranceScope(record map[string]any) {
	for _, scenario := range record["scenarios"].([]map[string]any) {
		scenario["affected_modules"] = []string{"pkg/a"}
		evidence := scenario["evidence"].([]map[string]any)
		evidence[0]["module_scope"] = []string{"pkg/a"}
	}
}

func markScenarioRiskAccepted(record map[string]any, scenarioIndex, approvalScenarioIndex int) {
	scenarios := record["scenarios"].([]map[string]any)
	scenarios[scenarioIndex]["status"] = "accepted risk"
	scenarios[scenarioIndex]["accepted_risks"] = []string{"GL-RISK-001"}
	record["verdict"] = "ready with named accepted risks"
	record["risk_acceptances"] = []map[string]any{{
		"id":           "GL-RISK-001",
		"accepted_by":  "release owner",
		"accepted_utc": "2026-08-12T00:00:00Z",
		"rationale":    "bounded test risk",
		"scenarios":    []string{scenarios[approvalScenarioIndex]["id"].(string)},
	}}
}

func markScenariosPassed(t *testing.T, root string, record map[string]any) {
	t.Helper()
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	digestScript := filepath.Join(scripts, "gate-input-digest.sh")
	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	if err := os.WriteFile(digestScript, []byte(`#!/bin/sh
[ "$GOLIB_ASSURANCE_GO_VERSION" = "go1.test" ] || exit 3
[ "$GOLIB_ASSURANCE_GOOS" = "testos" ] || exit 3
[ "$GOLIB_ASSURANCE_GOARCH" = "testarch" ] || exit 3
[ "$GOLIB_ASSURANCE_CGO_ENABLED" = "0" ] || exit 3
[ "$GOLIB_ASSURANCE_KERNEL" = "Test Kernel" ] || exit 3
[ "$GOLIB_ASSURANCE_NODE" = "missing" ] || exit 3
case "$2" in
  pkg/a) printf '%s\n' '`+digestA+`' ;;
  pkg/b) printf '%s\n' '`+digestB+`' ;;
  pkg/fixture) printf '%s\n' '`+strings.Repeat("c", 64)+`' ;;
  *) exit 2 ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "evidence.txt")
	if err := os.WriteFile(artifact, []byte("current evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("current evidence\n"))
	evidence := []map[string]any{{
		"path":         "evidence.txt",
		"sha256":       hex.EncodeToString(digest[:]),
		"observed_utc": "2026-08-12T00:00:00Z",
		"environment":  "test",
		"input_environment": map[string]string{
			"go_version":  "go1.test",
			"goos":        "testos",
			"goarch":      "testarch",
			"cgo_enabled": "0",
			"kernel":      "Test Kernel",
			"node":        "missing",
		},
		"module_scope": []string{"*"},
		"input_digests": map[string]string{
			"pkg/a": digestA,
			"pkg/b": digestB,
		},
	}}
	for _, scenario := range record["scenarios"].([]map[string]any) {
		scenario["status"] = "passed"
		scenario["evidence"] = evidence
	}
}
