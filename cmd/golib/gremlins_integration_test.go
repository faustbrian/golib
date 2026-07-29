package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGolibGremlinsIntegrationModeTestsTargetSubpackage(t *testing.T) {
	root := testRepositoryRoot(t)
	module := standaloneModule(t, "package fixture\n")
	worker := filepath.Join(module, "worker")
	if err := os.Mkdir(worker, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(worker, "worker.go"), `package worker

func IsZero(value int) bool {
	return value == 0
}
`)
	writeFile(t, filepath.Join(worker, "worker_test.go"), `package worker

import "testing"

func TestIsZero(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("zero was not recognized")
	}
	if IsZero(1) {
		t.Fatal("non-zero value was recognized as zero")
	}
}
`)

	build := exec.Command(filepath.Join(root, "scripts", "build-golib-gremlins.sh"))
	build.Dir = root
	output, err := build.Output()
	if err != nil {
		t.Fatalf("build golib-gremlins: %v", err)
	}

	report := filepath.Join(t.TempDir(), "mutation.json")
	command := exec.Command(
		strings.TrimSpace(string(output)),
		"unleash",
		"./worker",
		"--integration",
		"--coverpkg",
		"./worker",
		"--workers",
		"1",
		"--test-cpu",
		"1",
		"--timeout-coefficient",
		"10",
		"--threshold-efficacy",
		"100",
		"--threshold-mcover",
		"100",
		"--conditionals-negation",
		"--output-statuses",
		"lctvsr",
		"--output",
		report,
	)
	command.Dir = module
	command.Env = environmentWith("GOWORK", "off")
	if output, err = command.CombinedOutput(); err != nil {
		t.Fatalf("execute subpackage mutant: %v\n%s", err, output)
	}

	contents, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	result := struct {
		Files []struct {
			Mutations []struct {
				Status string `json:"status"`
			} `json:"mutations"`
		} `json:"files"`
	}{}
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode mutation report: %v", err)
	}
	mutations := 0
	for _, file := range result.Files {
		for _, mutation := range file.Mutations {
			mutations++
			if mutation.Status != "KILLED" {
				t.Fatalf("subpackage mutant has status %s", mutation.Status)
			}
		}
	}
	if mutations == 0 {
		t.Fatal("golib-gremlins did not discover the subpackage mutant")
	}
}

func TestGolibGremlinsSkipsIntegrationWhenUnitTestKillsMutant(t *testing.T) {
	module := gremlinsConditionalFixture(t)
	writeFile(t, filepath.Join(module, "fixture_test.go"), gremlinsIsZeroUnitTest)
	writeFile(t, filepath.Join(module, "integration_test.go"), gremlinsIntegrationMarker)
	marker := filepath.Join(t.TempDir(), "integration-ran")
	output, err := runGremlinsIntegrationFixture(t, module, marker)
	if err != nil {
		t.Fatalf("execute unit-killed mutant: %v\n%s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("integration suite ran for unit-killed mutant: %v", err)
	}
}

func TestGolibGremlinsRunsIntegrationWhenUnitTestDoesNotKillMutant(t *testing.T) {
	module := gremlinsConditionalFixture(t)
	writeFile(t, filepath.Join(module, "fixture_test.go"), `package fixture

import "testing"

func TestUnitSuite(t *testing.T) {
	t.Log("unit suite does not exercise IsZero")
}
`)
	writeFile(
		t,
		filepath.Join(module, "integration_test.go"),
		gremlinsIntegrationMarker+gremlinsIsZeroTestFunction,
	)
	marker := filepath.Join(t.TempDir(), "integration-ran")
	output, err := runGremlinsIntegrationFixture(t, module, marker)
	if err != nil {
		t.Fatalf("execute integration-killed mutant: %v\n%s", err, output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("integration suite did not run for unit-surviving mutant: %v", err)
	}
}

func TestGolibGremlinsRejectsFailingUnitBaselineBeforeMutation(t *testing.T) {
	module := gremlinsConditionalFixture(t)
	writeFile(t, filepath.Join(module, "fixture_test.go"), `//go:build !integration

package fixture

import "testing"

func TestBrokenUnitBaseline(t *testing.T) {
	t.Fatal("broken unit baseline")
}
`)
	writeFile(
		t,
		filepath.Join(module, "integration_test.go"),
		gremlinsIntegrationTestHeader+gremlinsIsZeroTestFunction,
	)
	output, err := runGremlinsIntegrationFixture(t, module, "")
	if err == nil {
		t.Fatalf("failing unit baseline was accepted:\n%s", output)
	}
}

func gremlinsConditionalFixture(t *testing.T) string {
	t.Helper()

	return standaloneModule(t, `package fixture

func IsZero(value int) bool {
	return value == 0
}
`)
}

func runGremlinsIntegrationFixture(
	t *testing.T,
	module, marker string,
) ([]byte, error) {
	t.Helper()

	profile := filepath.Join(t.TempDir(), "coverage.out")
	coverage := exec.Command(
		"go",
		"test",
		"-tags",
		"integration",
		"-coverpkg=.",
		"-coverprofile="+profile,
		".",
	)
	coverage.Dir = module
	coverage.Env = environmentWith("GOWORK", "off")
	if output, err := coverage.CombinedOutput(); err != nil {
		t.Fatalf("generate external coverage: %v\n%s", err, output)
	}

	root := testRepositoryRoot(t)
	build := exec.Command(filepath.Join(root, "scripts", "build-golib-gremlins.sh"))
	build.Dir = root
	output, err := build.Output()
	if err != nil {
		t.Fatalf("build golib-gremlins: %v", err)
	}
	command := exec.Command(
		strings.TrimSpace(string(output)),
		"unleash",
		".",
		"--integration",
		"--coverpkg",
		".",
		"--workers",
		"1",
		"--test-cpu",
		"1",
		"--timeout-coefficient",
		"10",
		"--threshold-efficacy",
		"100",
		"--threshold-mcover",
		"100",
		"--conditionals-negation",
		"--output-statuses",
		"lctvsr",
		"--output",
		filepath.Join(t.TempDir(), "mutation.json"),
		"--tags",
		"integration",
	)
	command.Dir = module
	command.Env = environmentWith("GOWORK", "off")
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_GREMLINS_COVERAGE_PROFILE",
		profile,
	)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_GREMLINS_COVERAGE_ELAPSED",
		"1s",
	)
	if marker != "" {
		command.Env = environmentWithValues(
			command.Env,
			"GREMLINS_MUTANT_PHASE",
			"1",
		)
		command.Env = environmentWithValues(
			command.Env,
			"GREMLINS_INTEGRATION_MARKER",
			marker,
		)
	}

	return command.CombinedOutput()
}

const gremlinsIsZeroUnitTest = `package fixture

import "testing"

func TestIsZero(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("zero was not recognized")
	}
	if IsZero(1) {
		t.Fatal("non-zero value was recognized as zero")
	}
}
`

const gremlinsIntegrationTestHeader = `//go:build integration

package fixture

import "testing"

`

const gremlinsIntegrationMarker = `//go:build integration

package fixture

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GREMLINS_MUTANT_PHASE") == "1" {
		if err := os.WriteFile(os.Getenv("GREMLINS_INTEGRATION_MARKER"), []byte("ran"), 0o600); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
`

const gremlinsIsZeroTestFunction = `
func TestIsZero(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("zero was not recognized")
	}
	if IsZero(1) {
		t.Fatal("non-zero value was recognized as zero")
	}
}
`
