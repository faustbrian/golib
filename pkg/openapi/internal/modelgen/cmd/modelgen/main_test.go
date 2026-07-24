package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/internal/modelgen"
	"github.com/faustbrian/golib/pkg/openapi/internal/specification"
)

func FuzzModelgenFieldInventoryDecoder(f *testing.F) {
	header := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n"
	for _, seed := range []string{
		header,
		header + "one\t3.2.0\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\ttrue\tdescription\n",
		"",
		"wrong\theader\n",
		"\"unterminated",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = decodeFields(bytes.NewReader(raw))
	})
}

func TestDecodeFieldsEnforcesExactByteLimit(t *testing.T) {
	t.Parallel()

	header := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n"
	exact := header + strings.Repeat(
		"\n", maximumFieldInventoryBytes-len(header),
	)
	if _, err := decodeFields(strings.NewReader(exact)); err != nil {
		t.Fatalf("exact-limit inventory error = %v", err)
	}
	if _, err := decodeFields(strings.NewReader(exact + "\n")); err == nil {
		t.Fatal("decodeFields accepted oversized input")
	}
}

func TestRunGeneratesModelsAndSurfaceTestsForEveryVersion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	conformance := filepath.Join(root, "specification", "conformance")
	if err := os.MkdirAll(conformance, 0o755); err != nil {
		t.Fatal(err)
	}
	fields := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n" +
		"one\t3.2.0\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\ttrue\tdescription\n" +
		"two\t3.1.2\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\ttrue\tdescription\n" +
		"three\t3.0.4\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\ttrue\tdescription\n" +
		"four\t2.0\tsource\t1\tSwagger Object\tFixed Fields\tswagger\tstring\tfalse\ttrue\tdescription\n"
	if err := os.WriteFile(
		filepath.Join(conformance, "object-fields.tsv"),
		[]byte(fields),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := run(root); err != nil {
		t.Fatal(err)
	}
	for _, packageName := range []string{"oas30", "oas31", "oas32", "swagger20"} {
		for _, filename := range []string{"model_generated.go", "model_generated_test.go"} {
			if _, err := os.Stat(filepath.Join(root, packageName, filename)); err != nil {
				t.Errorf("%s/%s: %v", packageName, filename, err)
			}
		}
	}
}

func TestReadFieldsRejectsMalformedInventories(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"wrong\theader\n",
		"id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n" +
			"one\t3.2.0\tsource\tnan\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\ttrue\tdescription\n",
		"id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n" +
			"one\t3.2.0\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tbad\ttrue\tdescription\n",
		"id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n" +
			"one\t3.2.0\tsource\t1\tOpenAPI Object\tFixed Fields\topenapi\tstring\tfalse\tbad\tdescription\n",
		"id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\ntruncated\n",
	}
	for index, contents := range tests {
		path := filepath.Join(t.TempDir(), "fields.tsv")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readFields(path); err == nil {
			t.Errorf("case %d error = nil", index)
		}
	}
	if _, err := readFields(filepath.Join(t.TempDir(), "missing.tsv")); err == nil {
		t.Fatal("missing inventory error = nil")
	}
	path := filepath.Join(t.TempDir(), "oversized.tsv")
	header := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n"
	if err := os.WriteFile(
		path,
		[]byte(header+strings.Repeat("\n", 8_388_608-len(header)+1)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := readFields(path); err == nil {
		t.Fatal("oversized inventory error = nil")
	}
}

func TestRunRejectsInvalidInventoryAndOutputLayouts(t *testing.T) {
	t.Parallel()

	if err := run(t.TempDir()); err == nil {
		t.Fatal("run accepted a missing inventory")
	}
	invalid := t.TempDir()
	writeModelgenInventory(t, invalid,
		"one\t3.2.0\tsource\t1\tOpenAPI Object\tFixed Fields\tbad\tMystery\tfalse\ttrue\tdescription\n")
	if err := run(invalid); err == nil {
		t.Fatal("run accepted an unmapped field type")
	}
	blockedPackage := t.TempDir()
	writeModelgenInventory(t, blockedPackage, "")
	if err := os.WriteFile(filepath.Join(blockedPackage, "oas32"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(blockedPackage); err == nil {
		t.Fatal("run accepted a package path that is a file")
	}
	blockedOutput := t.TempDir()
	writeModelgenInventory(t, blockedOutput, "")
	if err := os.MkdirAll(filepath.Join(blockedOutput, "oas32", "model_generated.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(blockedOutput); err == nil {
		t.Fatal("run replaced a generated-source directory")
	}
	blockedTestOutput := t.TempDir()
	writeModelgenInventory(t, blockedTestOutput, "")
	if err := os.MkdirAll(
		filepath.Join(blockedTestOutput, "oas32", "model_generated_test.go"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := run(blockedTestOutput); err == nil {
		t.Fatal("run replaced a generated-test directory")
	}
}

func TestRunPropagatesTestGenerationFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModelgenInventory(t, root, "")
	want := errors.New("generate tests")
	if err := runWith(root, func(
		modelgen.Config,
		[]specification.ObjectField,
	) ([]byte, error) {
		return nil, want
	}); !errors.Is(err, want) {
		t.Fatalf("test generation error = %v", err)
	}
}

type failingTemporaryFile struct {
	name     string
	writeErr error
	closeErr error
}

func (file failingTemporaryFile) Write([]byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return 1, nil
}

func (file failingTemporaryFile) Close() error { return file.closeErr }

func (file failingTemporaryFile) Name() string { return file.name }

func TestWriteAndCloseTemporaryPropagatesFailures(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("write")
	closeErr := errors.New("close")
	if err := writeAndCloseTemporary(failingTemporaryFile{
		writeErr: writeErr, closeErr: closeErr,
	}, []byte("value")); !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("joined write error = %v", err)
	}
	if err := writeAndCloseTemporary(failingTemporaryFile{
		closeErr: closeErr,
	}, []byte("value")); !errors.Is(err, closeErr) {
		t.Fatalf("close error = %v", err)
	}
	if err := writeAtomicWith(
		filepath.Join(t.TempDir(), "output.go"),
		[]byte("value"),
		func(string, string) (temporaryFile, error) {
			return failingTemporaryFile{
				name: filepath.Join(t.TempDir(), "temporary"), writeErr: writeErr,
			}, nil
		},
	); !errors.Is(err, writeErr) {
		t.Fatalf("atomic write error = %v", err)
	}
}

func TestWriteAtomicReplacesFilesAndRejectsDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	destination := filepath.Join(root, "generated.go")
	if err := writeAtomic(destination, []byte("package first\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(destination, []byte("package second\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || string(raw) != "package second\n" {
		t.Fatalf("generated file = %q, error = %v", raw, err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(directory, []byte("content")); err == nil {
		t.Fatal("writeAtomic replaced a directory")
	}
}

func TestMainAndExecuteReportStatus(t *testing.T) {
	root := t.TempDir()
	writeModelgenInventory(t, root, "")
	var stderr strings.Builder
	if status := execute(t.TempDir(), &stderr); status != 1 || stderr.Len() == 0 {
		t.Fatalf("failed execute status = %d, stderr = %q", status, stderr.String())
	}
	stderr.Reset()
	if status := execute(root, &stderr); status != 0 || stderr.Len() != 0 {
		t.Fatalf("successful execute status = %d, stderr = %q", status, stderr.String())
	}
	priorRoot := *rootFlag
	priorExit := exitProcess
	t.Cleanup(func() {
		*rootFlag = priorRoot
		exitProcess = priorExit
	})
	*rootFlag = root
	status := -1
	exitProcess = func(code int) { status = code }
	main()
	if status != 0 {
		t.Fatalf("main exit status = %d", status)
	}
}

func writeModelgenInventory(t *testing.T, root string, rows string) {
	t.Helper()
	directory := filepath.Join(root, "specification", "conformance")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	header := "id\tversion\tsource\tline\tobject\tvariant\tname\ttype\tpattern\trequired\tdescription\n"
	if _, err := io.WriteString(modelgenFile(t, filepath.Join(directory, "object-fields.tsv")), header+rows); err != nil {
		t.Fatal(err)
	}
}

func modelgenFile(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestWriteAtomicRejectsMissingDirectory(t *testing.T) {
	t.Parallel()

	if err := writeAtomic(
		filepath.Join(t.TempDir(), "missing", "file.go"),
		[]byte("package generated\n"),
	); err == nil {
		t.Fatal("writeAtomic error = nil")
	}
}
