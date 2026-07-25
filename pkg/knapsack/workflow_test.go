package knapsack_test

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"testing"
)

var actionCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestWorkflowsPinExternalActionsByCommit(t *testing.T) {
	t.Parallel()
	if actions := pinnedWorkflowActions(t); len(actions) == 0 {
		t.Fatal("knapsack workflows contain no external actions")
	}
}

func TestNilAwayRunsAsAdvisoryModuleGate(t *testing.T) {
	t.Parallel()
	contract, err := os.ReadFile("../../scripts/check-module.sh")
	if err != nil {
		t.Fatal(err)
	}
	gates, err := os.ReadFile("../../scripts/check-gates.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gates), "nilaway\n") {
		t.Fatal("canonical module contract omits the NilAway advisory gate")
	}
	for _, required := range []string{
		"NilAway advisory exit status",
		"set +e",
	} {
		if !strings.Contains(string(contract), required) {
			t.Fatalf("NilAway advisory gate omits %q", required)
		}
	}
	workflow, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workflow), "continue-on-error") ||
		!strings.Contains(string(workflow), "./scripts/run-modules.sh check") {
		t.Fatal("root workflow must use the explicit advisory module gate")
	}
}

func TestReleaseEventRunsStrictRootContract(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"release:",
		"types: [published]",
		"go run ./cmd/golib select --all --format matrix",
		"./scripts/run-modules.sh check",
		"name: Required",
	} {
		if !strings.Contains(string(workflow), required) {
			t.Fatalf("release-event contract omits %q", required)
		}
	}
}

func pinnedWorkflowActions(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range []string{"../../.github/workflows/ci.yml"} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			value, step := strings.CutPrefix(line, "- uses: ")
			if !step {
				value, step = strings.CutPrefix(line, "uses: ")
			}
			if !step || strings.HasPrefix(value, "./") {
				continue
			}
			reference, version, documented := strings.Cut(value, " # ")
			action, commit, pinned := strings.Cut(reference, "@")
			if action == "" || !pinned || !actionCommit.MatchString(commit) ||
				!documented || version == "" {
				t.Errorf("%s contains an unpinned action: %s", path, value)
				continue
			}
			pin := commit + " # " + version
			if previous, exists := result[action]; exists && previous != pin {
				t.Errorf("%s uses conflicting pins for %s", path, action)
				continue
			}
			result[action] = pin
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}

	return result
}
