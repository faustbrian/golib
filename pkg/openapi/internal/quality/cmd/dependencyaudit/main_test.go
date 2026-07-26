package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyEvidenceAcceptsExactBuildList(t *testing.T) {
	t.Parallel()

	evidence := strings.NewReader(
		"module\tversion\tclass\tlicense\tlicense-source\tsource\towner\tmaintenance\trelease\tnecessity\treplacement\n" +
			"example.test/direct\tv1.2.3\truntime\tMIT\thttps://example.test/LICENSE\thttps://example.test/source\tExample\tactive\ttags\tparser\tstandard library\n" +
			"example.test/graph\tv2.0.0\tgraph-only\tBSD-3-Clause\thttps://example.test/LICENSE\thttps://example.test/source\tExample\tactive\ttags\tupstream graph\tremove upstream edge\n",
	)
	modules := []module{
		{Path: "example.test/direct", Version: "v1.2.3"},
		{Path: "example.test/graph", Version: "v2.0.0", Indirect: true},
	}

	if err := verifyEvidence(evidence, modules); err != nil {
		t.Fatalf("verifyEvidence() error = %v", err)
	}
}

func TestVerifyEvidenceRejectsDriftAndIncompleteRows(t *testing.T) {
	t.Parallel()

	modules := []module{{Path: "example.test/dependency", Version: "v1.2.3"}}
	base := "module\tversion\tclass\tlicense\tlicense-source\tsource\towner\tmaintenance\trelease\tnecessity\treplacement\n"
	valid := "\tMIT\thttps://example.test/LICENSE\thttps://example.test/source\tOwner\tactive\ttags\tneed\treplace\n"
	for _, test := range []struct {
		name string
		rows string
	}{
		{name: "missing", rows: ""},
		{name: "unexpected", rows: "example.test/other\tv1.2.3\truntime" + valid},
		{name: "version", rows: "example.test/dependency\tv1.2.4\truntime" + valid},
		{name: "duplicate", rows: strings.Repeat("example.test/dependency\tv1.2.3\truntime"+valid, 2)},
		{name: "empty field", rows: "example.test/dependency\tv1.2.3\truntime\tMIT\thttps://example.test/LICENSE\thttps://example.test/source\t\tactive\ttags\tneed\treplace\n"},
		{name: "invalid class", rows: "example.test/dependency\tv1.2.3\ttest" + valid},
		{name: "insecure license source", rows: "example.test/dependency\tv1.2.3\truntime\tMIT\thttp://example.test/LICENSE\thttps://example.test/source\tOwner\tactive\ttags\tneed\treplace\n"},
		{name: "insecure source", rows: "example.test/dependency\tv1.2.3\truntime\tMIT\thttps://example.test/LICENSE\thttp://example.test/source\tOwner\tactive\ttags\tneed\treplace\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := verifyEvidence(strings.NewReader(base+test.rows), modules); err == nil {
				t.Fatal("verifyEvidence accepted invalid evidence")
			}
		})
	}
}

func TestDecodeModulesRejectsAmbiguousOrUnboundedOutput(t *testing.T) {
	t.Parallel()

	if _, err := decodeModules(strings.NewReader(`{"Path":"example.test/a","Version":"v1.0.0"}{"Path":`)); err == nil {
		t.Fatal("decodeModules accepted malformed output")
	}
	prefix := `{"Path":"example.test/a","Version":"v1.0.0"}`
	exact := prefix + strings.Repeat(" ", maximumModuleListBytes-len(prefix))
	if modules, err := decodeModules(strings.NewReader(exact)); err != nil || len(modules) != 1 {
		t.Fatalf("exact-limit module list = %#v, %v", modules, err)
	}
	if _, err := decodeModules(strings.NewReader(exact + " ")); err == nil {
		t.Fatal("decodeModules accepted oversized output")
	}
	if _, err := decodeModules(failingReader{}); err == nil {
		t.Fatal("decodeModules accepted a reader failure")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read") }

func TestDecodeModulesRejectsMissingPath(t *testing.T) {
	t.Parallel()

	if _, err := decodeModules(strings.NewReader(`{"Version":"v1.0.0"}`)); err == nil {
		t.Fatal("decodeModules accepted a missing module path")
	}
}

func TestVerifyEvidenceRejectsMalformedHeaderAndUnversionedModules(t *testing.T) {
	t.Parallel()

	if err := verifyEvidence(strings.NewReader(""), nil); err == nil {
		t.Fatal("verifyEvidence accepted missing evidence")
	}
	if err := verifyEvidence(strings.NewReader("bad\theader\n"), nil); err == nil {
		t.Fatal("verifyEvidence accepted an invalid header")
	}
	if err := verifyEvidence(strings.NewReader("\"unterminated"), nil); err == nil {
		t.Fatal("verifyEvidence accepted malformed TSV")
	}
	if err := verifyEvidence(failingReader{}, nil); err == nil {
		t.Fatal("verifyEvidence accepted a reader failure")
	}
	header := strings.Join(evidenceHeader, "\t") + "\n"
	if err := verifyEvidence(
		strings.NewReader(header),
		[]module{{Path: "example.test/unversioned"}},
	); err == nil {
		t.Fatal("verifyEvidence accepted an unversioned module")
	}
}

func TestVerifyEvidenceEnforcesExactByteLimit(t *testing.T) {
	t.Parallel()

	header := strings.Join(evidenceHeader, "\t") + "\n"
	prefix := "example.test/a\tv1.0.0\truntime\tMIT\t" +
		"https://example.test/LICENSE\thttps://example.test/source\t" +
		"Owner\tactive\ttags\tneed\t"
	padding := strings.Repeat("x", maximumEvidenceBytes-len(header)-len(prefix))
	exact := header + prefix + padding
	modules := []module{{Path: "example.test/a", Version: "v1.0.0"}}
	if len(exact) != maximumEvidenceBytes {
		t.Fatalf("evidence bytes = %d", len(exact))
	}
	if err := verifyEvidence(strings.NewReader(exact), modules); err != nil {
		t.Fatalf("exact-limit evidence error = %v", err)
	}
	if err := verifyEvidence(strings.NewReader(exact+"x"), modules); err == nil {
		t.Fatal("verifyEvidence accepted oversized evidence")
	}
}

func TestOpenEvidenceFileReadsAFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dependencies.tsv")
	if err := os.WriteFile(path, []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openEvidenceFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEqualRecordClassifiesLengthContentAndEquality(t *testing.T) {
	t.Parallel()

	if equalRecord([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("equalRecord accepted different lengths")
	}
	if equalRecord([]string{"a"}, []string{"b"}) {
		t.Fatal("equalRecord accepted different content")
	}
	if !equalRecord([]string{"a"}, []string{"a"}) {
		t.Fatal("equalRecord rejected equal records")
	}
}

func TestIsHTTPSRejectsMalformedHostlessAndCredentialedURLs(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"%zz", "http://example.test/source", "https:source",
		"https://user:secret@example.test/source",
	} {
		if isHTTPS(raw) {
			t.Fatalf("isHTTPS accepted %q", raw)
		}
	}
	if !isHTTPS("https://example.test/source") {
		t.Fatal("isHTTPS rejected HTTPS URL")
	}
}

func TestLimitedBufferEnforcesItsLimit(t *testing.T) {
	t.Parallel()

	buffer := &limitedBuffer{remaining: 3}
	if written, err := buffer.Write([]byte("abc")); err != nil || written != 3 {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if _, err := buffer.Write([]byte("d")); err == nil {
		t.Fatal("Write accepted output beyond the limit")
	}
}

func TestListModulesCoversSuccessAndExecutionFailure(t *testing.T) {
	t.Parallel()

	modules, err := listModules(".")
	if err != nil || len(modules) == 0 {
		t.Fatalf("listModules(.) = %#v, %v", modules, err)
	}
	if _, err := listModules(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("listModules accepted a missing working directory")
	}
}

type testEvidenceFile struct {
	io.Reader
}

func (testEvidenceFile) Close() error { return nil }

func TestExecuteCoversEveryOutcome(t *testing.T) {
	originalLoad := loadModules
	originalOpen := openEvidence
	t.Cleanup(func() {
		loadModules = originalLoad
		openEvidence = originalOpen
	})
	var stderr bytes.Buffer
	if code := execute([]string{"-unknown"}, &stderr); code != 2 {
		t.Fatalf("flag exit = %d", code)
	}
	loadModules = func(string) ([]module, error) {
		return nil, errors.New("list")
	}
	if code := execute(nil, &stderr); code != 1 {
		t.Fatalf("list exit = %d", code)
	}
	loadModules = func(string) ([]module, error) {
		return []module{{Path: "example.test/a", Version: "v1.0.0"}}, nil
	}
	openEvidence = func(string) (evidenceFile, error) {
		return nil, errors.New("open")
	}
	if code := execute(nil, &stderr); code != 1 {
		t.Fatalf("open exit = %d", code)
	}
	openEvidence = func(string) (evidenceFile, error) {
		return testEvidenceFile{Reader: strings.NewReader("bad\n")}, nil
	}
	if code := execute(nil, &stderr); code != 1 {
		t.Fatalf("invalid evidence exit = %d", code)
	}
	row := strings.Join(evidenceHeader, "\t") + "\n" +
		"example.test/a\tv1.0.0\truntime\tMIT\thttps://example.test/LICENSE\t" +
		"https://example.test/source\tOwner\tactive\ttags\tneed\treplace\n"
	openEvidence = func(string) (evidenceFile, error) {
		return testEvidenceFile{Reader: strings.NewReader(row)}, nil
	}
	if code := execute([]string{"-root", t.TempDir()}, &stderr); code != 0 {
		t.Fatalf("success exit = %d, stderr = %s", code, stderr.String())
	}
}

func TestMainDelegatesToExit(t *testing.T) {
	originalArgs := os.Args
	originalExit := exitProcess
	originalLoad := loadModules
	originalOpen := openEvidence
	t.Cleanup(func() {
		os.Args = originalArgs
		exitProcess = originalExit
		loadModules = originalLoad
		openEvidence = originalOpen
	})
	os.Args = []string{"dependencyaudit"}
	loadModules = func(string) ([]module, error) { return nil, nil }
	openEvidence = func(string) (evidenceFile, error) {
		return testEvidenceFile{Reader: strings.NewReader(strings.Join(evidenceHeader, "\t") + "\n")}, nil
	}
	got := -1
	exitProcess = func(code int) { got = code }
	main()
	if got != 0 {
		t.Fatalf("main exit = %d", got)
	}
}
