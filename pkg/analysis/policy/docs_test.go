package policy_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/policy"
)

func TestDocumentation(t *testing.T) {
	t.Parallel()

	root := os.DirFS("..")
	rules := readDocumentation(t, root, "docs/rules.md")
	governance := readDocumentation(t, root, "docs/governance.md")
	hardening := readDocumentation(t, root, "docs/hardening.md")
	rollout := readDocumentation(t, root, "docs/rollout.md")
	reference := readDocumentation(t, root, "docs/reference.md")
	readme := readDocumentation(t, root, "README.md")
	contributing := readDocumentation(t, root, "CONTRIBUTING.md")
	security := readDocumentation(t, root, "SECURITY.md")
	compatibility := readDocumentation(t, root, "docs/compatibility.md")
	customRules := readDocumentation(t, root, "docs/custom-rules.md")
	faq := readDocumentation(t, root, "docs/faq.md")
	changelog := readDocumentation(t, root, "CHANGELOG.md")
	corpus := readDocumentation(t, root, "docs/corpus.md")
	release := readDocumentation(t, root, "docs/release.md")

	registry, err := policy.Builtin()
	if err != nil {
		t.Fatalf("Builtin() error = %v", err)
	}
	for _, entry := range registry.Entries() {
		heading := "## `" + entry.Rule.ID + "`"
		if count := strings.Count(rules, heading); count != 1 {
			t.Errorf("rule catalog contains %d headings for %s", count, entry.Rule.ID)
		}
		for _, overlap := range entry.Overlaps {
			parts := strings.Split(overlap.Tool, "/")
			identifier := parts[len(parts)-1]
			if !strings.Contains(governance, identifier) {
				t.Errorf("governance matrix does not mention %s", overlap.Tool)
			}
		}
		if count := strings.Count(hardening, "`"+entry.Rule.ID+"`"); count == 0 {
			t.Errorf("hardening matrix omits %s", entry.Rule.ID)
		}
	}

	requiredLinks := []string{
		"[contributor guide](CONTRIBUTING.md)",
		"[security policy](SECURITY.md)",
		"[changelog](CHANGELOG.md)",
		"[complete rule catalog](docs/rules.md)",
		"[command, API, SARIF, and performance reference](docs/reference.md)",
		"[rule governance and conflict matrix](docs/governance.md)",
		"[organization hardening evidence](docs/hardening.md)",
		"[repository rollout and advisory promotion](docs/rollout.md)",
		"[corpus precision and performance](docs/corpus.md)",
		"[release process](docs/release.md)",
		"[compatibility policy](docs/compatibility.md)",
		"[custom-rule design](docs/custom-rules.md)",
		"[FAQ](docs/faq.md)",
	}
	for _, link := range requiredLinks {
		if !strings.Contains(readme, link) {
			t.Errorf("README does not contain %s", link)
		}
	}
	if !strings.Contains(governance, "golangci-lint `interfacebloat`") {
		t.Error("governance does not assign interface breadth authority")
	}
	if !strings.Contains(rollout, "zero unexplained findings") {
		t.Error("rollout does not require a clean corpus before promotion")
	}
	for _, command := range []string{
		"analysis check",
		"analysis rules",
		"analysis validate-config",
		"analysis version",
	} {
		if !strings.Contains(reference, command) {
			t.Errorf("reference does not document %s", command)
		}
	}
	requiredEvidence := map[string]struct {
		document string
		phrase   string
	}{
		"contributor mutation gate":     {contributing, "zero surviving"},
		"security target execution":     {security, "does not execute target"},
		"compatibility rule IDs":        {compatibility, "rule IDs"},
		"custom rule analysistestkit":   {customRules, "analysistestkit"},
		"FAQ advisory explanation":      {faq, "Fixture precision"},
		"changelog unreleased section":  {changelog, "## Unreleased"},
		"corpus parallel determinism":   {corpus, "once with normal package parallelism"},
		"release artifact verification": {release, "packages the candidate twice"},
	}
	for name, evidence := range requiredEvidence {
		if !strings.Contains(evidence.document, evidence.phrase) {
			t.Errorf("missing %s", name)
		}
	}
}

func readDocumentation(t *testing.T, filesystem fs.FS, path string) string {
	t.Helper()

	contents, err := fs.ReadFile(filesystem, path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(contents)
}
