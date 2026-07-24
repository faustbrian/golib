package phone_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/phone"
)

func TestParseInternationalNumberSeparatesCanonicalAndDisplayForms(t *testing.T) {
	t.Parallel()

	number, err := phone.Parse("+1 650 253 0000", phone.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if number.E164() != "+16502530000" || !number.Possible() || !number.Valid() {
		t.Fatalf("number = %q, possible %v, valid %v", number.E164(), number.Possible(), number.Valid())
	}
	if number.Region().String() != "US" || number.CallingCode().String() != "+1" {
		t.Fatalf("region/calling code = %q, %q", number.Region(), number.CallingCode())
	}
	if number.Type() != phone.TypeFixedLineOrMobile {
		t.Fatalf("Type() = %v, want fixed-line-or-mobile", number.Type())
	}
	national, err := number.Format(phone.FormatNational)
	if err != nil || national != "(650) 253-0000" {
		t.Fatalf("national format = %q, %v", national, err)
	}
	internationalDisplay, err := number.Format(phone.FormatInternational)
	if err != nil || internationalDisplay != "+1 650-253-0000" {
		t.Fatalf("international format = %q, %v", internationalDisplay, err)
	}
}

func TestNationalParsingRequiresExplicitRegionHint(t *testing.T) {
	t.Parallel()

	if _, err := phone.Parse("040 1234567", phone.ParseOptions{}); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Parse without hint error = %v, want ErrInvalid", err)
	}
	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatalf("country.Parse(FI) error = %v", err)
	}
	number, err := phone.Parse("040 1234567", phone.ParseOptions{RegionHint: finland})
	if err != nil {
		t.Fatalf("Parse with hint error = %v", err)
	}
	if number.E164() != "+358401234567" || number.Region().String() != "FI" || number.Type() != phone.TypeMobile {
		t.Fatalf("number = %q, region %q, type %v", number.E164(), number.Region(), number.Type())
	}
}

func TestExtensionsRemainSeparateFromE164Identity(t *testing.T) {
	t.Parallel()

	number, err := phone.Parse("+1 650 253 0000 ext. 123", phone.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if number.E164() != "+16502530000" || number.Extension() != "123" {
		t.Fatalf("E164/extension = %q / %q", number.E164(), number.Extension())
	}
	display, err := number.Format(phone.FormatInternational)
	if err != nil || !strings.Contains(display, "ext. 123") {
		t.Fatalf("international extension format = %q, %v", display, err)
	}
}

func TestPossibleAndValidAreDistinctMetadataDecisions(t *testing.T) {
	t.Parallel()

	number, err := phone.Parse("+1 200 555 0123", phone.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !number.Possible() || number.Valid() {
		t.Fatalf("possible/valid = %v/%v, want true/false", number.Possible(), number.Valid())
	}
}

func TestParseE164RejectsNonCanonicalRepresentations(t *testing.T) {
	t.Parallel()

	number, err := phone.ParseE164("+16502530000")
	if err != nil || number.E164() != "+16502530000" {
		t.Fatalf("ParseE164() = %q, %v", number.E164(), err)
	}
	for _, input := range []string{"16502530000", "+1 6502530000", "+16502530000 ext 1"} {
		if _, err := phone.ParseE164(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseE164(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestPhoneParsingIsBoundedAndRedacted(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "+", "not a number", "\xff123", strings.Repeat("1", phone.MaxBytes+1)} {
		_, err := phone.Parse(input, phone.ParseOptions{})
		if !errors.Is(err, international.ErrInvalid) && !errors.Is(err, international.ErrResourceLimit) {
			t.Errorf("Parse input error = %v, want invalid or resource limit", err)
		}
	}

	number, err := phone.ParseE164("+16502530000")
	if err != nil {
		t.Fatalf("ParseE164() error = %v", err)
	}
	if got := fmt.Sprint(number); got != "[phone]" {
		t.Fatalf("default formatting = %q, want redacted", got)
	}
	if got := fmt.Sprintf("%#v", number); got != "phone.Number{redacted}" {
		t.Fatalf("Go formatting = %q, want redacted", got)
	}
	if _, err := number.Format(phone.FormatStyle(255)); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("unknown format error = %v, want ErrInvalid", err)
	}
	tooLongExtension := "+1 650 253 0000 ext. " + strings.Repeat("1", phone.MaxExtensionBytes+1)
	if _, err := phone.Parse(tooLongExtension, phone.ParseOptions{}); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("oversized extension error = %v, want ErrInvalid", err)
	}
}

func TestCallingCodesAreTypedAndValidated(t *testing.T) {
	t.Parallel()

	code, err := phone.ParseCallingCode("+358")
	if err != nil {
		t.Fatalf("ParseCallingCode() error = %v", err)
	}
	if code.String() != "+358" || code.Int() != 358 || code.IsZero() {
		t.Fatalf("calling code = %q (%d), IsZero %v", code, code.Int(), code.IsZero())
	}
	for _, input := range []string{"", "358", "+0", "+999", "+35A", "+1234"} {
		if _, err := phone.ParseCallingCode(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseCallingCode(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestZeroPhoneHasAbsentSemantics(t *testing.T) {
	t.Parallel()

	var number phone.Number
	if !number.IsZero() || number.E164() != "" || number.Extension() != "" || number.Possible() ||
		number.Valid() || !number.Region().IsZero() || !number.CallingCode().IsZero() ||
		number.Type() != phone.TypeUnknown || number.String() != "[phone]" {
		t.Fatalf("zero phone is not absent: %#v", number)
	}
	if _, err := number.Format(phone.FormatNational); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Format(zero) error = %v, want ErrInvalid", err)
	}
	var callingCode phone.CallingCode
	if callingCode.String() != "" || callingCode.Int() != 0 || !callingCode.IsZero() {
		t.Fatalf("zero calling code = %q (%d)", callingCode, callingCode.Int())
	}
}

func TestPhoneMetadataProvenanceIsPinned(t *testing.T) {
	t.Parallel()

	provenance := phone.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
	if provenance.UpstreamVersion != "phonenumbers v1.8.1; libphonenumber v9.0.32" {
		t.Fatalf("version = %q", provenance.UpstreamVersion)
	}
}
