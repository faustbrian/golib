package yamlwire

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
)

func TestBoundaryPredicates(t *testing.T) {
	t.Parallel()

	for _, indent := range []int{0, 2, 9} {
		if !validIndent(indent) {
			t.Fatalf("validIndent(%d) = false", indent)
		}
	}
	for _, indent := range []int{-1, 1, 10} {
		if validIndent(indent) {
			t.Fatalf("validIndent(%d) = true", indent)
		}
	}
	if hasBlockScalar(-1) || !hasBlockScalar(0) {
		t.Fatal("hasBlockScalar() did not preserve the sentinel boundary")
	}
	if exceedsLimit(4, 4) || !exceedsLimit(5, 4) {
		t.Fatal("exceedsLimit() did not preserve the exact boundary")
	}
	if got := outputCapacity(4, 2); got != 6 {
		t.Fatalf("outputCapacity() = %d, want 6", got)
	}
	if depthLimitEnabled(0) || !depthLimitEnabled(1) {
		t.Fatal("depthLimitEnabled() did not preserve the zero boundary")
	}
	if exceedsDepth(2, 2) || !exceedsDepth(3, 2) {
		t.Fatal("exceedsDepth() did not preserve the exact boundary")
	}
}

func TestAliasLimitPredicates(t *testing.T) {
	t.Parallel()

	if needsLimitPlugin(DecodeOptions{}) {
		t.Fatal("needsLimitPlugin() enabled an unconfigured plugin")
	}
	for _, options := range []DecodeOptions{{DisallowAliases: true}, {MaxAliases: 1}, {MaxDepth: 1}} {
		if !needsLimitPlugin(options) {
			t.Fatalf("needsLimitPlugin(%+v) = false", options)
		}
	}
	if aliasLimitEnabled(false, 0) || !aliasLimitEnabled(true, 0) || !aliasLimitEnabled(false, 1) {
		t.Fatal("aliasLimitEnabled() did not preserve independent controls")
	}
	if aliasesDisabled(false, 1) || aliasesDisabled(true, 0) || !aliasesDisabled(true, 1) {
		t.Fatal("aliasesDisabled() did not preserve the first-alias boundary")
	}
	if aliasesExceeded(0, 1) || aliasesExceeded(1, 1) || !aliasesExceeded(1, 2) {
		t.Fatal("aliasesExceeded() did not preserve the configured boundary")
	}
}

func TestBlockScalarIndicatorBoundaries(t *testing.T) {
	t.Parallel()

	for input, expected := range map[string]int{
		"x: |\n":    3,
		"- |\n":     2,
		"x: |+\n":   3,
		"x: >-\r\n": 3,
	} {
		if got := blockScalarIndicator([]byte(input)); got != expected {
			t.Fatalf("blockScalarIndicator(%q) = %d, want %d", input, got, expected)
		}
	}
	for _, input := range []string{"", "|", " |", "x: x", "xx |", "x:|"} {
		if got := blockScalarIndicator([]byte(input)); got != -1 {
			t.Fatalf("blockScalarIndicator(%q) = %d, want -1", input, got)
		}
	}
}

func TestAddBlockIndentIndicatorsHonorsExactCapacity(t *testing.T) {
	t.Parallel()

	payload := []byte("text: |-\n  value\n")
	if _, err := addBlockIndentIndicators(payload, 2, int64(len(payload))); !errors.Is(err, outputlimit.ErrLimit) {
		t.Fatalf("addBlockIndentIndicators() over-limit error = %v", err)
	}
	got, err := addBlockIndentIndicators(payload, 2, int64(len(payload)+1))
	if err != nil {
		t.Fatalf("addBlockIndentIndicators() exact limit error = %v", err)
	}
	if string(got) != "text: |2-\n  value\n" {
		t.Fatalf("addBlockIndentIndicators() = %q", got)
	}
}
