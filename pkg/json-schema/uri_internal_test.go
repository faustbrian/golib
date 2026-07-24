package jsonschema

import (
	"fmt"
	"net/url"
	"testing"
)

func TestNormalizeURLAppliesRFCIdentityRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{
			input: "HTTP://user@EXAMPLE.TEST:80/a/./b/../c/%7e/%2f?q=%7e%2f#%7e%2f",
			want:  "http://user@example.test/a/c/~/%2F?q=~%2F#~%2F",
		},
		{input: "HTTPS://[2001:DB8::1]:443/schema", want: "https://[2001:db8::1]/schema"},
		{input: "a/b/../c", want: "a/c"},
		{input: "URN:EXAMPLE:%7e", want: "urn:EXAMPLE:~"},
	}

	for _, testCase := range tests {
		parsed, err := url.Parse(testCase.input)
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := normalizeURL(parsed)
		if err != nil {
			t.Fatal(err)
		}
		if actual := normalized.String(); actual != testCase.want {
			t.Errorf("%q: got %q, want %q", testCase.input, actual, testCase.want)
		}
	}
}

func TestNormalizeURLRejectsInvalidInternalInputs(t *testing.T) {
	t.Parallel()

	if _, err := normalizeURL(nil); err == nil {
		t.Fatal("nil URI was accepted")
	}
	for _, parsed := range []*url.URL{
		{RawQuery: "%"},
		{Opaque: "%"},
	} {
		if _, err := normalizeURL(parsed); err == nil {
			t.Fatal("malformed URI component was accepted")
		}
	}
	for _, value := range []string{"%", "%0", "%GG"} {
		if _, err := normalizePercentEncoding(value); err == nil {
			t.Errorf("%q: expected percent-encoding error", value)
		}
	}
	if _, valid := hexadecimalValue('x'); valid {
		t.Fatal("non-hexadecimal byte was accepted")
	}
}

func TestRemoveDotSegmentsPreservesURIPathStructure(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"../a":         "a",
		"./a":          "a",
		"/a/./b":       "/a/b",
		"/a/.":         "/a/",
		"/a/b/../c":    "/a/c",
		"/a/b/..":      "/a/",
		".":            "",
		"..":           "",
		"//a//b":       "//a//b",
		"a/b/../../..": "/",
	}
	for input, want := range tests {
		if actual := removeDotSegments(input); actual != want {
			t.Errorf("%q: got %q, want %q", input, actual, want)
		}
	}
}

func TestHexadecimalValueRecognizesExactlyHexadecimalBytes(t *testing.T) {
	t.Parallel()

	for value := 0; value <= 255; value++ {
		character := byte(value)
		actual, valid := hexadecimalValue(character)
		var want byte
		wantValid := true
		switch {
		case character >= '0' && character <= '9':
			want = character - '0'
		case character >= 'a' && character <= 'f':
			want = character - 'a' + 10
		case character >= 'A' && character <= 'F':
			want = character - 'A' + 10
		default:
			wantValid = false
		}
		if actual != want || valid != wantValid {
			t.Errorf("byte %d: got (%d, %t), want (%d, %t)",
				value, actual, valid, want, wantValid)
		}
	}
}

func TestURIUnreservedRecognizesExactlyRFC3986Set(t *testing.T) {
	t.Parallel()

	const unreserved = "abcdefghijklmnopqrstuvwxyz" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	for value := 0; value <= 255; value++ {
		character := byte(value)
		want := false
		for index := range len(unreserved) {
			want = want || character == unreserved[index]
		}
		if actual := uriUnreserved(character); actual != want {
			t.Errorf("byte %d: got %t, want %t", value, actual, want)
		}
	}
}

func TestNormalizePercentEncodingCoversEveryByte(t *testing.T) {
	t.Parallel()

	for value := 0; value <= 255; value++ {
		encoded := fmt.Sprintf("%%%02x", value)
		actual, err := normalizePercentEncoding(encoded)
		if err != nil {
			t.Fatalf("%q: %v", encoded, err)
		}
		character := byte(value)
		var want string
		if uriUnreserved(character) {
			want = string(character)
		} else {
			want = fmt.Sprintf("%%%02X", value)
		}
		if actual != want {
			t.Errorf("%q: got %q, want %q", encoded, actual, want)
		}
	}
}

func TestRemoveDotSegmentsCoversSegmentBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":            "",
		"a":           "a",
		"/":           "/",
		"/a":          "/a",
		"a/":          "a/",
		"/a/../b":     "/b",
		"a/../b":      "/b",
		"/../a":       "/a",
		"/../../a":    "/a",
		"/a/b/../../": "/",
	}
	for input, want := range tests {
		if actual := removeDotSegments(input); actual != want {
			t.Errorf("%q: got %q, want %q", input, actual, want)
		}
	}
}
