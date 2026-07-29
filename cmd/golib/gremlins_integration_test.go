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
