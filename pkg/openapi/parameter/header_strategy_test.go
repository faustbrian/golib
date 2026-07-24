package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestRecommendHeaderEncodingSelectsTextForRiskyValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  parameter.HeaderEncodingStrategy
	}{
		{value: "gzip", want: parameter.HeaderSchema},
		{value: "a/b?c=d", want: parameter.HeaderSchema},
		{value: "a%20b", want: parameter.HeaderSchema},
		{value: "a;b", want: parameter.HeaderSchema},
		{value: `attachment; filename="a b.txt"`,
			want: parameter.HeaderTextPlainContent},
		{value: "value with space", want: parameter.HeaderTextPlainContent},
		{value: "snowman ☃", want: parameter.HeaderTextPlainContent},
	} {
		actual, err := parameter.RecommendHeaderEncoding(test.value, 1_000)
		if err != nil {
			t.Fatal(err)
		}
		if actual != test.want {
			t.Fatalf("RecommendHeaderEncoding(%q) = %v, want %v",
				test.value, actual, test.want)
		}
	}
}

func TestRecommendHeaderEncodingValidatesValuesAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		value   string
		maximum int
		want    error
	}{
		{name: "maximum", value: "value", want: parameter.ErrInvalidHeaderValue},
		{name: "UTF-8", value: string([]byte{0xff}), maximum: 1,
			want: parameter.ErrInvalidHeaderValue},
		{name: "newline", value: "one\ntwo", maximum: 100,
			want: parameter.ErrInvalidHeaderValue},
		{name: "limit", value: "value", maximum: 4,
			want: parameter.ErrHeaderValueLimit},
	} {
		_, err := parameter.RecommendHeaderEncoding(test.value, test.maximum)
		if !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}

	actual, err := parameter.RecommendHeaderEncoding("value", 5)
	if err != nil || actual != parameter.HeaderSchema {
		t.Fatalf("exact limit = %v, %v", actual, err)
	}
	actual, err = parameter.RecommendHeaderEncoding("A", 1)
	if err != nil || actual != parameter.HeaderSchema {
		t.Fatalf("minimum limit = %v, %v", actual, err)
	}
}
