package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCheckAcceptsCompleteMutationEvidence(t *testing.T) {
	t.Parallel()

	report := `{
		"files":[{"mutations":[{"status":"KILLED"},{"status":"NOT_VIABLE"}]}],
        "mutants_killed": 42,
		"mutants_total": 45,
        "mutants_lived": 0,
        "mutants_not_covered": 0,
        "mutants_not_viable": 3,
        "mutants_timed_out": 0,
        "mutants_skipped": 0,
        "test_efficacy": 100,
        "mutations_coverage": 100
    }`
	if err := check(strings.NewReader(report)); err != nil {
		t.Fatalf("check() error = %v", err)
	}
}

func TestCheckRejectsIncompleteMutationEvidence(t *testing.T) {
	t.Parallel()

	for _, report := range []string{
		`{`,
		`{} {}`,
		`{"mutants_killed":0,"test_efficacy":100,"mutations_coverage":100}`,
		`{"mutants_total":0,"mutants_killed":1,"test_efficacy":100,"mutations_coverage":100}`,
		`{"mutants_total":1,"mutants_killed":0,"test_efficacy":100,"mutations_coverage":100}`,
		`{"mutants_total":2,"mutants_killed":1,"mutants_lived":1,"test_efficacy":50,"mutations_coverage":100}`,
		`{"mutants_total":2,"mutants_killed":1,"mutants_not_covered":1,"test_efficacy":100,"mutations_coverage":50}`,
		`{"mutants_total":1,"mutants_killed":1,"mutants_timed_out":1,"test_efficacy":100,"mutations_coverage":100}`,
		`{"mutants_total":1,"mutants_killed":1,"mutants_skipped":1,"test_efficacy":100,"mutations_coverage":100}`,
		`{"mutants_total":1,"mutants_killed":1,"test_efficacy":99.99,"mutations_coverage":100}`,
		`{"files":[{"mutations":[{"status":"TIMED OUT"}]}],"mutants_total":1,"mutants_killed":1,"test_efficacy":100,"mutations_coverage":100}`,
	} {
		if err := check(strings.NewReader(report)); err == nil {
			t.Fatalf("check accepted incomplete evidence %s", report)
		}
	}
}

func TestCheckDoesNotExposeUnknownMutantStatus(t *testing.T) {
	t.Parallel()

	const secret = "private-status-token"
	report := `{"files":[{"mutations":[{"status":"` + secret +
		`"}]}],"mutants_total":1,"mutants_killed":1,` +
		`"test_efficacy":100,"mutations_coverage":100}`
	err := check(strings.NewReader(report))
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unredacted mutation report error = %v", err)
	}
}

func FuzzMutationReportDecoder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"mutants_total":1,"mutants_killed":1,"test_efficacy":100,"mutations_coverage":100}`,
		`{"files":[{"mutations":[{"status":"KILLED"}]}]}`,
		`[]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = decodeReport(bytes.NewReader(raw), 64<<10)
	})
}

func TestDecodeReportAcceptsExactLimitAndClassifiesTrailingValues(t *testing.T) {
	t.Parallel()

	const maximum = 256
	prefix := `{"padding":"`
	suffix := `","mutants_total":1,"mutants_killed":1}`
	report := prefix + strings.Repeat("x", maximum-len(prefix)-len(suffix)) + suffix
	if len(report) != maximum {
		t.Fatalf("report bytes = %d", len(report))
	}
	decoded, err := decodeReport(strings.NewReader(report), maximum)
	if err != nil || decoded.MutantsKilled != 1 {
		t.Fatalf("exact-limit report = %#v, %v", decoded, err)
	}
	if _, err := decodeReport(
		strings.NewReader(`{} {}`), maximum,
	); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing report error = %v", err)
	}
}

type testInput struct {
	io.Reader
	closeErr error
}

func (input testInput) Close() error { return input.closeErr }

func TestExecuteCoversUsageInputAndLifecycleOutcomes(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if code := execute(nil, &stderr, nil); code != 2 {
		t.Fatalf("usage exit = %d", code)
	}
	openErr := errors.New("open")
	if code := execute([]string{"report"}, &stderr, func(string) (inputFile, error) {
		return nil, openErr
	}); code != 1 {
		t.Fatalf("open failure exit = %d", code)
	}
	if code := execute([]string{"report"}, &stderr, func(string) (inputFile, error) {
		return testInput{Reader: strings.NewReader(`{}`)}, nil
	}); code != 1 {
		t.Fatalf("invalid report exit = %d", code)
	}
	valid := `{"mutants_total":1,"mutants_killed":1,"test_efficacy":100,"mutations_coverage":100}`
	if code := execute([]string{"report"}, &stderr, func(string) (inputFile, error) {
		return testInput{Reader: strings.NewReader(valid), closeErr: errors.New("close")}, nil
	}); code != 1 {
		t.Fatalf("close failure exit = %d", code)
	}
	if code := execute([]string{"report"}, &stderr, func(string) (inputFile, error) {
		return testInput{Reader: strings.NewReader(valid)}, nil
	}); code != 0 {
		t.Fatalf("success exit = %d", code)
	}
}

func TestOpenReportReadsAFile(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/report.json"
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openReport(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMainDelegatesToTheProcessExit(t *testing.T) {
	originalArgs := os.Args
	originalExit := exitProcess
	originalOpen := openInput
	t.Cleanup(func() {
		os.Args = originalArgs
		exitProcess = originalExit
		openInput = originalOpen
	})
	os.Args = []string{"mutationgate", "report"}
	openInput = func(string) (inputFile, error) {
		return testInput{Reader: strings.NewReader(
			`{"mutants_total":1,"mutants_killed":1,"test_efficacy":100,"mutations_coverage":100}`,
		)}, nil
	}
	got := -1
	exitProcess = func(code int) { got = code }
	main()
	if got != 0 {
		t.Fatalf("main exit = %d", got)
	}
}
