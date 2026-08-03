package phone

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/nyaruka/phonenumbers"
)

func TestParseInputByteBoundary(t *testing.T) {
	t.Parallel()

	if _, err := Parse(strings.Repeat("x", MaxBytes), ParseOptions{}); errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Parse(exact byte limit) error = %v, want non-resource parse result", err)
	}
	if _, err := Parse(strings.Repeat("x", MaxBytes+1), ParseOptions{}); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Parse(over byte limit) error = %v, want ErrResourceLimit", err)
	}
}

func TestSnapshotDependencyMetadataBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		extension string
		wantError bool
	}{
		{"extension at limit", strings.Repeat("1", MaxExtensionBytes), false},
		{"extension over limit", strings.Repeat("1", MaxExtensionBytes+1), true},
		{"non-decimal extension", "a", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := parseTestPhoneNumber(t)
			parsed.Extension = &test.extension
			_, gotErr := snapshotParsed(parsed)
			if (gotErr != nil) != test.wantError {
				t.Fatalf("snapshotParsed() error = %v, wantError %v", gotErr, test.wantError)
			}
		})
	}

	for _, test := range []struct {
		value     int32
		wantError bool
	}{
		{-1, true},
		{0, true},
		{999, false},
		{1000, true},
	} {
		parsed := parseTestPhoneNumber(t)
		parsed.CountryCode = &test.value
		_, gotErr := snapshotParsed(parsed)
		if (gotErr != nil) != test.wantError {
			t.Errorf("snapshotParsed(country code %d) error = %v, wantError %v", test.value, gotErr, test.wantError)
		}
	}
}

func parseTestPhoneNumber(t *testing.T) *phonenumbers.PhoneNumber {
	t.Helper()

	parsed, err := phonenumbers.Parse("+1 650 253 0000", "ZZ")
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCallingCodeSyntaxBoundaries(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "+1234", "358", "+35A", "+0"} {
		if _, err := ParseCallingCode(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseCallingCode(%q) error = %v, want ErrInvalid", input, err)
		}
	}
	for _, input := range []string{"+1", "+358"} {
		if _, err := ParseCallingCode(input); err != nil {
			t.Errorf("ParseCallingCode(%q) error = %v", input, err)
		}
	}
}

func TestDecimalCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  bool
	}{
		{"", true},
		{"/", false},
		{"0", true},
		{"9", true},
		{":", false},
	} {
		if got := decimal(test.input); got != test.want {
			t.Errorf("decimal(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestEncodedNumberValidationBranches(t *testing.T) {
	t.Parallel()

	valid := "+16502530000;ext=123"
	if parsed, err := parseEncodedNumber(valid); err != nil || parsed.encoded() != valid {
		t.Fatalf("parseEncodedNumber(valid) = %q, %v", parsed.encoded(), err)
	}
	atExtensionLimit := "+16502530000;ext=" + strings.Repeat("1", MaxExtensionBytes)
	if parsed, err := parseEncodedNumber(atExtensionLimit); err != nil || parsed.encoded() != atExtensionLimit {
		t.Fatalf("parseEncodedNumber(extension at limit) = %q, %v", parsed.encoded(), err)
	}

	invalid := []string{
		"+16502530000;ext=1;ext=2",
		"+16502530000;bad;ext=1",
		"+16502530000;ext=",
		"+16502530000;ext=" + strings.Repeat("1", MaxExtensionBytes+1),
		"+16502530000;ext=a",
		"+999;ext=1",
		"+1 650 253 0000;ext=1",
	}
	for _, input := range invalid {
		if _, err := parseEncodedNumber(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("parseEncodedNumber(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}
