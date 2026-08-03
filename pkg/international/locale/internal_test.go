package locale

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestParseResourceBoundaries(t *testing.T) {
	t.Parallel()

	atByteLimit := privateUseTag(11, 19)
	if len(atByteLimit) != MaxBytes {
		t.Fatalf("byte-limit fixture length = %d, want %d", len(atByteLimit), MaxBytes)
	}
	if _, err := Parse(atByteLimit); err != nil {
		t.Fatalf("Parse(exact byte limit) error = %v", err)
	}

	overByteLimit := privateUseTag(12, 18)
	if len(overByteLimit) != MaxBytes+1 {
		t.Fatalf("over-byte-limit fixture length = %d, want %d", len(overByteLimit), MaxBytes+1)
	}
	if _, err := Parse(overByteLimit); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Parse(over byte limit) error = %v, want ErrResourceLimit", err)
	}

	atSegmentLimit := "en-x-" + strings.Join(repeated("a", MaxSegments-2), "-")
	if _, err := Parse(atSegmentLimit); err != nil {
		t.Fatalf("Parse(exact segment limit) error = %v", err)
	}

	overSegmentLimit := "en-x-" + strings.Join(repeated("a", MaxSegments-1), "-")
	if _, err := Parse(overSegmentLimit); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Parse(over segment limit) error = %v, want ErrResourceLimit", err)
	}
}

func TestParseMalformedInputChecks(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "\xffn-US", "en_US"} {
		if _, err := Parse(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func privateUseTag(eightByteSegments, sevenByteSegments int) string {
	parts := append(repeated("aaaaaaaa", eightByteSegments), repeated("aaaaaaa", sevenByteSegments)...)
	return "en-x-" + strings.Join(parts, "-")
}

func repeated(value string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = value
	}
	return values
}
