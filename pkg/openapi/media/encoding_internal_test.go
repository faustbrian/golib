package media

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestScaledEncodingMaximumSaturatesAtTheIntegerLimit(t *testing.T) {
	t.Parallel()

	maximum := int(^uint(0) >> 1)
	threshold := maximum / 3
	if got := scaledEncodingMaximum(threshold); got != threshold*3 {
		t.Fatalf("scaled threshold = %d, want %d", got, threshold*3)
	}
	if got := scaledEncodingMaximum(threshold + 1); got != maximum {
		t.Fatalf("scaled overflow = %d, want %d", got, maximum)
	}
}

func TestMultipartNamePercentEncodingEndpoints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		character byte
		want      bool
	}{
		{character: 0x1f, want: true},
		{character: 0x20, want: false},
		{character: 0x7f, want: true},
		{character: 0x80, want: true},
	} {
		if got := multipartNameNeedsPercentEncoding(test.character); got != test.want {
			t.Fatalf("percent encoding for %#x = %t, want %t", test.character, got, test.want)
		}
	}
}

func TestEncodingMediaTypeMatchingRejectsMalformedInputs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern  string
		concrete string
	}{
		{pattern: "invalid", concrete: "application/json"},
		{pattern: "application/*", concrete: "invalid"},
	} {
		if encodingMediaTypeMatches(test.pattern, test.concrete) {
			t.Fatalf("encodingMediaTypeMatches(%q, %q) = true", test.pattern, test.concrete)
		}
	}
}

func TestEncodingMediaTypePartsRejectsEveryMalformedShape(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"invalid", "/json", "application/"} {
		if _, _, valid := encodingMediaTypeParts(value); valid {
			t.Fatalf("encodingMediaTypeParts(%q) was valid", value)
		}
	}
	major, subtype, valid := encodingMediaTypeParts("application/json")
	if !valid || major != "application" || subtype != "json" {
		t.Fatalf("encodingMediaTypeParts() = %q, %q, %t", major, subtype, valid)
	}
}

func TestInternationalLinkAttributeRequiresContent(t *testing.T) {
	t.Parallel()

	empty, err := jsonvalue.Object(nil)
	if err != nil {
		t.Fatal(err)
	}
	if validInternationalLinkAttribute(empty) {
		t.Fatal("empty international link attribute was accepted")
	}
}
