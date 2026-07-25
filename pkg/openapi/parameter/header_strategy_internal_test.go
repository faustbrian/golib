package parameter

import "testing"

func TestHeaderParameterRecognitionRespectsQuotedStrings(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "value; name=value", want: true},
		{value: "value;\tname \t=value", want: true},
		{value: `"ignored; name=value"`},
		{value: `"escaped \"; ignored=value"`},
		{value: `"ignored"; real=value`, want: true},
		{value: "value; name value"},
		{value: "value; =value"},
		{value: "value; invalid@=value"},
		{value: "value; name"},
		{value: "value;"},
		{value: "value"},
	} {
		if actual := hasHeaderParameter(test.value); actual != test.want {
			t.Fatalf("hasHeaderParameter(%q) = %t, want %t",
				test.value, actual, test.want)
		}
	}
}

func TestHeaderURISafetyAndHexadecimalBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"A", "%20", "%0F", "%F0", "%2f"} {
		if !headerValueIsURISafe(value) {
			t.Errorf("headerValueIsURISafe(%q) = false", value)
		}
	}
	for _, value := range []string{"%", "%2", "a%2", "%GG", "%GF", "%FG"} {
		if headerValueIsURISafe(value) {
			t.Errorf("headerValueIsURISafe(%q) = true", value)
		}
	}
	for _, value := range []byte{'0', '9', 'A', 'F', 'a', 'f'} {
		if !hexDigit(value) {
			t.Errorf("hexDigit(%q) = false", value)
		}
	}
	for _, value := range []byte{'/', ':', '@', 'G', '`', 'g'} {
		if hexDigit(value) {
			t.Errorf("hexDigit(%q) = true", value)
		}
	}
}
