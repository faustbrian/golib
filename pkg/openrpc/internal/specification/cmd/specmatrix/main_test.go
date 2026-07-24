package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRegeneratesReviewedMatrices(t *testing.T) {
	workspace := matrixWorkspace(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := run(); err != nil {
		t.Fatal(err)
	}
	main()
	for _, path := range []string{normativePath, fieldsPath} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), "complete") {
			t.Fatalf("%s did not include reviewed evidence: %s", path, contents)
		}
	}
}

func TestRunReportsMissingInputs(t *testing.T) {
	workspace := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := run(); err == nil || !strings.Contains(err.Error(), "read specification") {
		t.Fatalf("run error = %v", err)
	}
}

func TestRunMainReportsFailureAndExits(t *testing.T) {
	workspace := t.TempDir()
	withWorkspace(t, workspace)

	var stderr bytes.Buffer
	exitCode := 0
	runMain(&stderr, func(code int) { exitCode = code })
	if exitCode != 1 || !strings.Contains(stderr.String(), "read specification") {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
}

func TestRunReportsEveryInputAndOutputFailure(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		want    string
	}{
		{name: "schema read", prepare: prepareSpecificationOnly, want: "read schema"},
		{name: "schema invalid", prepare: prepareInvalidSchema, want: "decode OpenRPC schema"},
		{name: "normative evidence read", prepare: prepareWithoutEvidence, want: "read normative evidence"},
		{name: "normative evidence invalid", prepare: prepareInvalidNormativeEvidence, want: "apply normative evidence"},
		{name: "field evidence read", prepare: prepareWithoutFieldEvidence, want: "read field evidence"},
		{name: "field evidence invalid", prepare: prepareInvalidEvidence, want: "apply field evidence"},
		{name: "normative write", prepare: prepareNormativeDirectory, want: "write normative matrix"},
		{name: "fields write", prepare: prepareFieldsDirectory, want: "write object-field matrix"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			test.prepare(t, workspace)
			withWorkspace(t, workspace)
			if err := run(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", err, test.want)
			}
		})
	}
}

func matrixWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	for _, directory := range []string{
		"specification/openrpc-1.4.1", "specification/conformance",
	} {
		if err := os.MkdirAll(filepath.Join(workspace, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(specificationPath, "Values MUST be stable.\n")
	write(schemaPath, `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
	write(normativeEvidencePath, "id\timplementation\tevidence\tstatus\tnotes\nORPC-1.4-0001\tmodel.go:Value\tvalue_test.go:TestValue\tcomplete\tVerified.\n")
	write(fieldEvidencePath, "object\tmodel\tvalidation\tevidence\tstatus\n#\tmodel.go:Value\tvalidate.go:Value\tvalue_test.go:TestValue\tcomplete\n")
	return workspace
}

func withWorkspace(t *testing.T, workspace string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func prepareSpecificationOnly(t *testing.T, workspace string) {
	t.Helper()
	path := filepath.Join(workspace, specificationPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Values MUST work."), 0o644); err != nil {
		t.Fatal(err)
	}
}

func prepareInvalidSchema(t *testing.T, workspace string) {
	t.Helper()
	prepareSpecificationOnly(t, workspace)
	writeTestFile(t, workspace, schemaPath, "{")
}

func prepareWithoutEvidence(t *testing.T, workspace string) {
	t.Helper()
	prepareSpecificationOnly(t, workspace)
	writeTestFile(t, workspace, schemaPath, `{}`)
}

func prepareInvalidEvidence(t *testing.T, workspace string) {
	t.Helper()
	prepareWithoutFieldEvidence(t, workspace)
	writeTestFile(t, workspace, fieldEvidencePath, "invalid")
}

func prepareInvalidNormativeEvidence(t *testing.T, workspace string) {
	t.Helper()
	prepareWithoutEvidence(t, workspace)
	writeTestFile(t, workspace, normativeEvidencePath, "invalid")
}

func prepareWithoutFieldEvidence(t *testing.T, workspace string) {
	t.Helper()
	prepareWithoutEvidence(t, workspace)
	writeTestFile(t, workspace, normativeEvidencePath, "id\timplementation\tevidence\tstatus\tnotes\nORPC-1.4-0001\tmodel\tevidence\tcomplete\tVerified.\n")
}

func prepareNormativeDirectory(t *testing.T, workspace string) {
	t.Helper()
	prepareValidWorkspace(t, workspace)
	if err := os.Mkdir(filepath.Join(workspace, normativePath), 0o755); err != nil {
		t.Fatal(err)
	}
}

func prepareFieldsDirectory(t *testing.T, workspace string) {
	t.Helper()
	prepareValidWorkspace(t, workspace)
	if err := os.Mkdir(filepath.Join(workspace, fieldsPath), 0o755); err != nil {
		t.Fatal(err)
	}
}

func prepareValidWorkspace(t *testing.T, workspace string) {
	t.Helper()
	prepareWithoutFieldEvidence(t, workspace)
	writeTestFile(t, workspace, schemaPath, `{"properties":{"name":{"type":"string"}}}`)
	writeTestFile(t, workspace, fieldEvidencePath, "object\tmodel\tvalidation\tevidence\tstatus\n#\tmodel\tvalidation\tevidence\tcomplete\n")
}

func writeTestFile(t *testing.T, workspace string, path string, contents string) {
	t.Helper()
	fullPath := filepath.Join(workspace, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
