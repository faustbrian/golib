package jsonschema

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestFormatHelpersCoverSecurityAndSyntaxEdges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if valid, err := simpleFormatFunc(validUUID).Valid(ctx, "value"); valid || !errors.Is(err, context.Canceled) {
		t.Fatalf("got valid=%v, err=%v", valid, err)
	}

	assertFormatResult(t, validColor, false, "#12g", "#12345")
	assertFormatResult(t, validRegex, true, `\\`, `(?=a)a`, `(?<=a)b`, `[^]`)
	assertFormatResult(t, validRegex, false, `\q`, `(?P<x>a)`, `(?P=x)`, `(`)
	mailbox := func(value string) bool { return validMailbox(value, false) }
	assertFormatResult(t, mailbox, true,
		`"quoted\\\"value"@example.test`,
		`"quoted@value"@example.test`,
		`user@[127.0.0.1]`,
		`user@[IPv6:::1]`,
	)
	assertFormatResult(t, mailbox, false,
		`a@b@example.test`, `"unfinished@example.test`,
		`"trailing\"@example.test`, strings.Repeat("a", 65)+"@example.test",
		`.user@example.test`, `user..name@example.test`, `usér@example.test`,
		`user@[not-an-address]`,
	)
	if lastUnquotedAt(`"escaped\\\"@quoted"@example.test`) < 0 {
		t.Fatal("expected an unquoted separator")
	}
	if lastUnquotedAt(`"trailing\\`) != -1 {
		t.Fatal("expected dangling escape rejection")
	}
	assertFormatResult(t, func(value string) bool { return validLocalPart(value, true) },
		true, "usér", `"quoted\\value"`)
	assertFormatResult(t, func(value string) bool { return validLocalPart(value, true) },
		false, `"embedded"quote"`, "a\n", "a\x00", `"trailing\"`)

	assertFormatResult(t, validASCIILabel, false, "é", "bad_label")
	assertFormatResult(t, validIDNLabel, true,
		"l·l", "͵α", "א׳", "カ・タ", "١", "۱")
	assertFormatResult(t, validIDNLabel, false,
		"", "!", "ـ", "١۱", "·l", "a·a", "͵a", "׳א", "a・b")
	if !validIDNRune('\u200D') || validIDNRune('!') {
		t.Fatal("unexpected IDN rune classification")
	}

	for _, test := range []struct {
		url           *url.URL
		international bool
		valid         bool
	}{
		{url: &url.URL{}, valid: false},
		{url: &url.URL{Host: "example.test:"}, valid: false},
		{url: &url.URL{Host: "example.test:abc"}, valid: false},
		{url: &url.URL{Host: "a:b:c"}, valid: false},
		{url: &url.URL{Host: "example.test:443"}, valid: true},
		{url: &url.URL{Host: "[::1]"}, valid: true},
		{url: &url.URL{Host: "[fe80::1%zone]"}, valid: false},
		{url: &url.URL{Host: "é.example"}, international: true, valid: true},
	} {
		if actual := validIdentifierAuthority(test.url, test.international); actual != test.valid {
			t.Errorf("authority %q: got %v, want %v", test.url.Host, actual, test.valid)
		}
	}
	assertFormatResult(t, validURIReference, false,
		"https://example.test:/path", "https://example.test:abc/path",
		"https://a:b:c/path")
	assertFormatResult(t, validURITemplate, false, "{{x}}", "x}", "{x")
}

func TestRegexLimitIncludesExactBoundary(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRegexBytes = 3
	valid, err := validRegexWithLimits("abc", limits)
	if err != nil || !valid {
		t.Fatalf("exact limit: valid=%t err=%v", valid, err)
	}
	valid, err = validRegexWithLimits("abcd", limits)
	if valid || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("over limit: valid=%t err=%v", valid, err)
	}
}

func TestIDNAProfilesMustRoundTripLabelsAndPreserveBoundaries(t *testing.T) {
	t.Parallel()

	lookup := fakeIDNAProfile{unicode: "example"}
	registration := fakeIDNAProfile{ascii: "different"}
	if validPunycodeLabel("xn--example", lookup, registration) {
		t.Fatal("mismatched Punycode round trip was accepted")
	}
	if validIDNHostnameLabels("one.two", "single") {
		t.Fatal("IDNA conversion changed the label boundaries")
	}
}

func TestFormatBoundariesAreExact(t *testing.T) {
	t.Parallel()

	assertFormatResult(t, validTime, true,
		"23:59:59Z", "00:00:00+23:59", "23:59:60Z",
		"00:59:60+01:00", "22:59:60-01:00")
	assertFormatResult(t, validTime, false,
		"24:00:00Z", "00:60:00Z", "00:00:61Z",
		"00:00:00+24:00", "00:00:00+00:60")
	assertFormatResult(t, validLegacyTime, true, "23:59:59")
	assertFormatResult(t, validLegacyTime, false,
		"24:00:00", "00:60:00", "00:00:60")
	assertFormatResult(t, validDateTime, true, "2000-01-01T00:00:00Z")
	assertFormatResult(t, validDateTime, false,
		"T00:00:00Z", "2000-01-1T00:00:00Z", "2000-01-01X00:00:00Z")
	assertFormatResult(t, validRelativeJSONPointer, true,
		"0", "9", "10", "0#", "1/a")
	assertFormatResult(t, validRelativeJSONPointer, false,
		"", "/a", "a", "01", "1x")

	local64 := strings.Repeat("a", 64)
	assertFormatResult(t, validEmail, true, local64+"@example.test")
	assertFormatResult(t, validEmail, false,
		"@example.test", "a@", local64+"a@example.test")
	if lastUnquotedAt("@a@b") != -1 || lastUnquotedAt("a@b@c") != -1 {
		t.Fatal("duplicate unquoted separator was accepted")
	}
	if !validLocalPart(`""`, false) {
		t.Fatal("empty quoted local part was rejected")
	}

	host253 := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) +
		"." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	assertFormatResult(t, validHostname, true, host253)
	assertFormatResult(t, validHostname, false, host253+"d")
	assertFormatResult(t, validASCIILabel, true, "A", "Z", "a", "z", "0", "9", "a-b")
	assertFormatResult(t, validASCIILabel, false, "@", "[", "`", "{")
	assertFormatResult(t, validIDNLabel, true, "٠", "٩", "۰", "۹")
	assertFormatResult(t, validIDNLabel, false, "٠۰", "٩۹")

	assertFormatResult(t, validPercentEncoding, true,
		"%00", "%09", "%0a", "%0f", "%0A", "%0F", "x%41y")
	assertFormatResult(t, validPercentEncoding, false,
		"%", "%0", "%0g", "%0G", "%/0")
	for _, character := range []byte{'0', '9', 'a', 'f', 'A', 'F'} {
		if !isHex(character) {
			t.Fatalf("hex boundary %q rejected", character)
		}
	}
	for _, character := range []byte{'/', ':', '`', 'g', '@', 'G'} {
		if isHex(character) {
			t.Fatalf("non-hex boundary %q accepted", character)
		}
	}
	if !isASCII("\x7f") || isASCII("\u0080") {
		t.Fatal("unexpected ASCII boundary classification")
	}
}

type fakeIDNAProfile struct {
	unicode string
	ascii   string
}

func (profile fakeIDNAProfile) ToUnicode(string) (string, error) {
	return profile.unicode, nil
}

func (profile fakeIDNAProfile) ToASCII(string) (string, error) {
	return profile.ascii, nil
}

func TestUnknownDialectHasNoStandardFormats(t *testing.T) {
	t.Parallel()

	if standardFormatSupported(Dialect("unknown"), "date") {
		t.Fatal("unknown dialect unexpectedly enabled a standard format")
	}
}

func assertFormatResult(
	t *testing.T,
	checker func(string) bool,
	want bool,
	values ...string,
) {
	t.Helper()
	for _, value := range values {
		if actual := checker(value); actual != want {
			t.Errorf("%q: got %v, want %v", value, actual, want)
		}
	}
}
