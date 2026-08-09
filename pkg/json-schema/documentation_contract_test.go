package jsonschema_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSpecificationDecisionRegister(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("docs/specification-decisions.md")
	if err != nil {
		t.Fatal(err)
	}
	register := string(contents)
	if !strings.Contains(register, "## Unresolved decisions") {
		t.Error("specification decision register has no unresolved-decisions section")
	}

	heading := regexp.MustCompile(`(?m)^## (JSONSCHEMA-DEC-[0-9]{3}):`)
	matches := heading.FindAllStringSubmatchIndex(register, -1)
	if len(matches) != 15 {
		t.Fatalf("specification decision register has %d decisions, want 15", len(matches))
	}
	for index, match := range matches {
		identifier := register[match[2]:match[3]]
		expected := fmt.Sprintf("JSONSCHEMA-DEC-%03d", index+1)
		if identifier != expected {
			t.Errorf("decision %d identifier = %q, want %q", index+1, identifier, expected)
		}

		end := len(register)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		section := register[match[0]:end]
		for _, required := range []string{
			"Status and owner",
			"Source",
			"Classification",
			"Issue",
			"Credible interpretations",
			"Known peer behavior",
			"Selected behavior",
			"Security and resource consequences",
			"Compatibility and wire consequences",
			"Executable evidence",
			"Public surface",
			"Upstream record",
			"Reconsider when",
		} {
			if !strings.Contains(section, "| "+required+" |") {
				t.Errorf("%s does not contain field %q", identifier, required)
			}
		}
		if !strings.Contains(section, "https://") {
			t.Errorf("%s does not contain an authoritative URL", identifier)
		}
	}
}

func TestSpecificationDecisionRegisterIsLinkedFromContracts(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"README.md",
		"CONTRIBUTING.md",
		"docs/conformance.md",
		"docs/versioning.md",
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("ReadFile(%q) error = %v", path, err)
			continue
		}
		if !strings.Contains(string(contents), "specification-decisions.md") {
			t.Errorf("%s does not link to the specification decision register", path)
		}
	}
}
