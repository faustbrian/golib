package analysis

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsPathResolutionFailure(t *testing.T) {
	t.Parallel()

	_, err := loadConfig("analysis.yml", nil, func(string) (string, error) {
		return "", errors.New("working directory unavailable")
	})
	if err == nil || !strings.Contains(err.Error(), "resolve configuration path") {
		t.Fatalf("loadConfig() error = %v, want path resolution error", err)
	}
}

func TestReadConfigurationAcceptsExactSizeLimit(t *testing.T) {
	t.Parallel()

	contents := append(
		[]byte("version: 1\n#"),
		bytes.Repeat([]byte{'x'}, maxConfigurationBytes-len("version: 1\n#"))...,
	)
	path := filepath.Join(t.TempDir(), "analysis.yml")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := readConfiguration(path)
	if err != nil {
		t.Fatalf("readConfiguration() error = %v", err)
	}
	if len(got) != maxConfigurationBytes {
		t.Fatalf("len(readConfiguration()) = %d, want %d",
			len(got), maxConfigurationBytes)
	}
}

func TestGeneratedPolicyEnforcesPathLimit(t *testing.T) {
	t.Parallel()

	paths := make([]string, maxGeneratedPaths)
	for index := range paths {
		paths[index] = fmt.Sprintf("generated/file-%d.go", index)
	}
	config := Config{
		Version: 1,
		Generated: GeneratedPolicy{
			Exclude: true,
			Paths:   paths,
		},
	}
	if err := config.Validate(nil); err != nil {
		t.Fatalf("Validate(exact generated path limit) error = %v", err)
	}
	config.Generated.Paths = append(config.Generated.Paths, "generated/overflow.go")
	if err := config.Validate(nil); err == nil {
		t.Fatal("Validate() accepted generated paths above limit")
	}
}
