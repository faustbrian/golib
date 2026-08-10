package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSpecificationDecisionsAcceptsCompleteRegister(t *testing.T) {
	t.Parallel()

	root, current := validSpecificationDecisionFixture(t)
	if err := validateSpecificationDecisions(root, current); err != nil {
		t.Fatalf("validateSpecificationDecisions() error = %v", err)
	}
}

func TestValidateSpecificationDecisionsAcceptsGroupedTerminalSectionAndEvidencePrefix(t *testing.T) {
	t.Parallel()

	root, current := validSpecificationDecisionFixture(t)
	path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
	replaceFileText(t, path, "`TestSpecificationVector`", "`TestSpecification*`")
	replaceFileText(
		t,
		path,
		"## Unresolved decisions\n\nNone.",
		"## Unresolved and excluded behavior\n\nNo known material decision is unresolved.",
	)
	replaceFileText(
		t,
		path,
		"https://example.com/specification/1.0#behavior",
		"http://example.com/specification/1.0#behavior",
	)

	if err := validateSpecificationDecisions(root, current); err != nil {
		t.Fatalf("validateSpecificationDecisions() error = %v", err)
	}
}

func TestValidateSpecificationDecisionsAcceptsSupersededDecisionWithKnownReplacement(t *testing.T) {
	t.Parallel()

	root, current := validSpecificationDecisionFixture(t)
	path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
	replaceFileText(t, path, "`resolved`", "`superseded`")
	replaceFileText(
		t,
		path,
		"\n## Unresolved decisions",
		"\nReplaced by EXAMPLE-DEC-002.\n\n"+
			validDecisionEntry("EXAMPLE-DEC-002", "TestSpecificationVector")+
			"\n## Unresolved decisions",
	)

	if err := validateSpecificationDecisions(root, current); err != nil {
		t.Fatalf("validateSpecificationDecisions() error = %v", err)
	}
}

func TestParseSpecificationDecisionsSequencesEachIdentifierSeriesIndependently(t *testing.T) {
	t.Parallel()

	contents := "## WSDL11-DEC-001: First WSDL 1.1 decision\n\n" +
		"## WSDL11-DEC-002: Second WSDL 1.1 decision\n\n" +
		"## WSDL20-DEC-001: First WSDL 2.0 decision\n\n" +
		"## WSDL20-DEC-002: Second WSDL 2.0 decision\n"

	decisions, err := parseSpecificationDecisions(contents)
	if err != nil {
		t.Fatalf("parseSpecificationDecisions() error = %v", err)
	}
	if len(decisions) != 4 {
		t.Fatalf("parseSpecificationDecisions() count = %d, want 4", len(decisions))
	}
}

func TestParseSpecificationDecisionsRejectsGapWithinIdentifierSeries(t *testing.T) {
	t.Parallel()

	contents := "## WSDL11-DEC-001: First WSDL 1.1 decision\n\n" +
		"## WSDL20-DEC-001: First WSDL 2.0 decision\n\n" +
		"## WSDL11-DEC-003: Third WSDL 1.1 decision\n"

	_, err := parseSpecificationDecisions(contents)
	if err == nil || !strings.Contains(err.Error(), "WSDL11-DEC-003 has sequence 003, want 002") {
		t.Fatalf("parseSpecificationDecisions() error = %v, want WSDL11 sequence gap", err)
	}
}

func TestParseSpecificationDecisionsDoesNotTreatDecisionSeriesAsInventory(t *testing.T) {
	t.Parallel()

	decisions, err := parseSpecificationDecisions(
		"## UNRESOLVED-DEC-001: Explicit unresolved policy\n",
	)
	if err != nil {
		t.Fatalf("parseSpecificationDecisions() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].identifier != "UNRESOLVED-DEC-001" {
		t.Fatalf("parseSpecificationDecisions() = %#v, want unresolved decision series", decisions)
	}
}

func TestValidateUnresolvedDecisionInventoryAcceptsClosedDispositions(t *testing.T) {
	t.Parallel()

	for _, disposition := range []string{
		"None.",
		"No known material decision is unresolved.",
		"No known material ambiguity remains open.",
	} {
		if err := validateUnresolvedDecisionInventory(
			"# Decisions\n\n## Unresolved decisions\n\n" + disposition + "\n",
		); err != nil {
			t.Errorf("validateUnresolvedDecisionInventory(%q) error = %v", disposition, err)
		}
	}
}

func TestValidateSpecificationDecisionsFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string, current *catalog)
		wantError string
	}{
		{
			name: "provenance without specification metadata",
			mutate: func(t *testing.T, _ string, current *catalog) {
				t.Helper()
				current.Modules[0].Specifications = nil
			},
			wantError: "specification metadata",
		},
		{
			name: "missing decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "pkg/example/docs/specification-decisions.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "decision register",
		},
		{
			name: "readme does not link decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				mustWriteFile(t, filepath.Join(root, "pkg/example/README.md"), "# Example\n")
			},
			wantError: "README.md does not link",
		},
		{
			name: "missing readme",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "pkg/example/README.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "read README.md",
		},
		{
			name: "conformance documentation does not link decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				mustWriteFile(t, filepath.Join(root, "pkg/example/docs/conformance.md"), "# Conformance\n")
			},
			wantError: "docs/conformance.md does not link",
		},
		{
			name: "compatibility documentation does not link decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				mustWriteFile(t, filepath.Join(root, "pkg/example/docs/compatibility.md"), "# Compatibility\n")
			},
			wantError: "docs/compatibility.md does not link",
		},
		{
			name: "named compatibility documentation does not link decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				mustWriteFile(
					t,
					filepath.Join(root, "pkg/example/docs/compatibility-decisions.md"),
					"# Compatibility decisions\n",
				)
			},
			wantError: "docs/compatibility-decisions.md does not link",
		},
		{
			name: "contributing documentation does not link decision register",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				mustWriteFile(t, filepath.Join(root, "pkg/example/CONTRIBUTING.md"), "# Contributing\n")
			},
			wantError: "CONTRIBUTING.md does not link",
		},
		{
			name: "missing conformance corpus",
			mutate: func(t *testing.T, _ string, current *catalog) {
				t.Helper()
				current.Modules[0].ConformanceCorpora = nil
			},
			wantError: "conformance corpus",
		},
		{
			name: "missing provenance",
			mutate: func(t *testing.T, _ string, current *catalog) {
				t.Helper()
				current.Modules[0].Provenance = nil
			},
			wantError: "provenance",
		},
		{
			name: "duplicate identifier",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				mustAppendFile(t, path, "\n"+validDecisionEntry("EXAMPLE-DEC-001", "TestSpecificationVector"))
			},
			wantError: "duplicate decision identifier",
		},
		{
			name: "malformed identifier",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "EXAMPLE-DEC-001", "example decision")
			},
			wantError: "stable decision identifier",
		},
		{
			name: "missing required field",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "| Reconsider when |", "| Future review |")
			},
			wantError: "reconsider",
		},
		{
			name: "unresolved decision",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`unresolved`")
			},
			wantError: "unresolved",
		},
		{
			name: "missing unresolved decision inventory",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(
					t,
					path,
					"\n## Unresolved decisions\n\nNone.\n",
					"",
				)
			},
			wantError: "unresolved decision inventory",
		},
		{
			name: "noncanonical unresolved decision inventory heading",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "## Unresolved decisions", "## Unresolved issues")
			},
			wantError: "unresolved decision inventory",
		},
		{
			name: "open unresolved decision inventory",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(
					t,
					path,
					"None.",
					"The empty batch response policy remains unresolved.",
				)
			},
			wantError: "remains open",
		},
		{
			name: "empty unresolved decision inventory",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "None.", "")
			},
			wantError: "has no disposition",
		},
		{
			name: "duplicate unresolved decision inventory",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				mustAppendFile(t, path, "\n## Unresolved and excluded behavior\n\nNone.\n")
			},
			wantError: "more than one unresolved decision inventory",
		},
		{
			name: "nonterminal unresolved decision inventory",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				mustAppendFile(
					t,
					path,
					"\n"+validDecisionEntry("EXAMPLE-DEC-002", "TestSpecificationVector"),
				)
			},
			wantError: "must be the final level-two section",
		},
		{
			name: "unknown decision status",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`reviewed`")
			},
			wantError: "status",
		},
		{
			name: "status outside status field",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`reviewed`")
				replaceFileText(
					t,
					path,
					"\n## Unresolved decisions",
					"\nThe peer HTTP status is `resolved`.\n\n## Unresolved decisions",
				)
			},
			wantError: "recognized decision status",
		},
		{
			name: "multiple decision statuses",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`resolved` or `superseded`")
			},
			wantError: "more than one decision status",
		},
		{
			name: "superseded decision without replacement",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`superseded`")
			},
			wantError: "replacement",
		},
		{
			name: "superseded decision with unknown replacement",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`superseded`")
				replaceFileText(
					t,
					path,
					"\n## Unresolved decisions",
					"\nReplacement decision: EXAMPLE-DEC-002.\n\n## Unresolved decisions",
				)
			},
			wantError: "known replacement decision",
		},
		{
			name: "superseded decision with unrelated known reference",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`resolved`", "`superseded`")
				replaceFileText(
					t,
					path,
					"\n## Unresolved decisions",
					"\nSee EXAMPLE-DEC-002 for peer behavior.\n\n"+
						validDecisionEntry("EXAMPLE-DEC-002", "TestSpecificationVector")+
						"\n## Unresolved decisions",
				)
			},
			wantError: "known replacement decision",
		},
		{
			name: "missing executable evidence",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "`TestSpecificationVector`", "none")
			},
			wantError: "executable evidence",
		},
		{
			name: "unknown executable evidence",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/docs/specification-decisions.md")
				replaceFileText(t, path, "TestSpecificationVector", "TestMissingVector")
			},
			wantError: "TestMissingVector",
		},
		{
			name: "missing provenance file",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "pkg/example/specification/manifest.tsv")); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "manifest.tsv",
		},
		{
			name: "malformed provenance row",
			mutate: func(t *testing.T, root string, _ *catalog) {
				t.Helper()
				path := filepath.Join(root, "pkg/example/specification/manifest.tsv")
				mustAppendFile(t, path, "broken\t1.1\thttps://example.com/specification/1.1\tnot-a-digest\tpinned\n")
			},
			wantError: "sha256",
		},
		{
			name: "conformance gate disabled",
			mutate: func(t *testing.T, _ string, current *catalog) {
				t.Helper()
				current.Modules[0].Gates["conformance"] = false
			},
			wantError: "conformance gate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, current := validSpecificationDecisionFixture(t)
			test.mutate(t, root, &current)
			err := validateSpecificationDecisions(root, current)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantError)) {
				t.Fatalf("validateSpecificationDecisions() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestValidateSpecificationDecisionsIgnoresNonProductionAndNonSpecificationModules(t *testing.T) {
	t.Parallel()

	current := catalog{Modules: []module{
		{Directory: "pkg/plain", Kind: "public library"},
		{
			Directory:      "pkg/harness",
			Kind:           "interoperability harness",
			Specifications: []string{"Example specification"},
		},
	}}
	if err := validateSpecificationDecisions(t.TempDir(), current); err != nil {
		t.Fatalf("validateSpecificationDecisions() error = %v", err)
	}
}

func TestValidateSpecificationProvenanceRejectsMalformedNestedJSONDigest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "pkg/example/specification/manifest.json")
	mustWriteFile(t, path, `{
  "repository": "https://example.com/specification",
  "version": "1.0.0",
  "files": [
    {"path": "valid.json", "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
    {"path": "broken.json", "sha256": "not-a-digest"}
  ]
}
`)
	err := validateSpecificationProvenance(
		root,
		"pkg/example",
		"pkg/example/specification/manifest.json",
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha256") {
		t.Fatalf("validateSpecificationProvenance() error = %v, want invalid sha256", err)
	}
}

func TestValidateSpecificationJSONAcceptsReleaseAsVersionPin(t *testing.T) {
	t.Parallel()

	err := validateSpecificationJSON([]byte(`{
  "source": "https://example.com/specification/tree/v1.2.3",
  "release": "v1.2.3",
  "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}`))
	if err != nil {
		t.Fatalf("validateSpecificationJSON() error = %v", err)
	}
}

func TestSelectSpecificationDecisionModules(t *testing.T) {
	t.Parallel()

	current := catalog{Modules: []module{
		{Directory: "pkg/first", Path: canonicalRoot + "/pkg/first"},
		{Directory: "pkg/second", Path: canonicalRoot + "/pkg/second"},
	}}
	selected, err := selectSpecificationDecisionModules(current, false, "pkg/second,"+canonicalRoot+"/pkg/first")
	if err != nil {
		t.Fatalf("selectSpecificationDecisionModules() error = %v", err)
	}
	if len(selected.Modules) != 2 ||
		selected.Modules[0].Directory != "pkg/first" ||
		selected.Modules[1].Directory != "pkg/second" {
		t.Fatalf("selected modules = %#v", selected.Modules)
	}

	if _, err := selectSpecificationDecisionModules(current, false, "pkg/missing"); err == nil {
		t.Fatal("selectSpecificationDecisionModules() accepted an unknown module")
	}
	if _, err := selectSpecificationDecisionModules(current, false, ""); err == nil {
		t.Fatal("selectSpecificationDecisionModules() accepted no selection")
	}
	if _, err := selectSpecificationDecisionModules(current, true, "pkg/first"); err == nil {
		t.Fatal("selectSpecificationDecisionModules() accepted conflicting selection")
	}
}

func validSpecificationDecisionFixture(t *testing.T) (string, catalog) {
	t.Helper()

	root := t.TempDir()
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/docs/specification-decisions.md"),
		"# Specification decisions\n\n"+
			validDecisionEntry("EXAMPLE-DEC-001", "TestSpecificationVector")+
			"\n## Unresolved decisions\n\nNone.\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/README.md"),
		"# Example\n\nSee the [specification decisions](docs/specification-decisions.md).\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/docs/conformance.md"),
		"# Conformance\n\nSee the [specification decisions](specification-decisions.md).\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/docs/compatibility.md"),
		"# Compatibility\n\nSee the [specification decisions](specification-decisions.md).\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/CONTRIBUTING.md"),
		"# Contributing\n\nSee the [specification decisions](docs/specification-decisions.md).\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/specification/manifest.tsv"),
		"id\tversion\turl\tsha256\tstatus\nexample\t1.0\thttps://example.com/specification/1.0\t0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\tpinned\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/example/conformance_test.go"),
		"package example\n\nfunc TestSpecificationVector(t *testing.T) {}\n",
	)

	return root, catalog{Modules: []module{{
		Directory:          "pkg/example",
		Kind:               "public library",
		Specifications:     []string{"Example specification 1.0"},
		ConformanceCorpora: []string{"Example vectors"},
		Provenance:         []string{"pkg/example/specification/manifest.tsv"},
		Gates:              map[string]bool{"conformance": true},
	}}}
}

func validDecisionEntry(identifier, evidence string) string {
	return "## " + identifier + ": Canonical behavior\n\n" +
		"| Field | Decision |\n" +
		"| --- | --- |\n" +
		"| Status and owner | `resolved`; example maintainers |\n" +
		"| Source | Example 1.0 [normative text](https://example.com/specification/1.0#behavior) |\n" +
		"| Classification | Normative ambiguity |\n" +
		"| Issue | The source permits two observable interpretations. |\n" +
		"| Credible interpretations | Accept the first form or reject it. |\n" +
		"| Known peer behavior | Maintained peer implementations disagree. |\n" +
		"| Selected behavior | Reject the ambiguous form deterministically. |\n" +
		"| Security and resource consequences | Input and work remain bounded. |\n" +
		"| Compatibility and wire consequences | The rejected form is not accepted on the wire. |\n" +
		"| Executable evidence | `" + evidence + "` and the pinned conformance corpus |\n" +
		"| Public surface | `Parse` |\n" +
		"| Upstream record | No published erratum changes this decision. |\n" +
		"| Reconsider when | A new specification revision resolves the ambiguity. |\n"
}

func mustAppendFile(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		t.Fatal(err)
	}
}

func replaceFileText(t *testing.T, path, old, replacement string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(contents), old, replacement, 1)
	if updated == string(contents) {
		t.Fatalf("fixture does not contain %q", old)
	}
	mustWriteFile(t, path, updated)
}
