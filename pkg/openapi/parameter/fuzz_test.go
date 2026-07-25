package parameter_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func FuzzOpenAPI32QueryDecode(f *testing.F) {
	for _, seed := range []struct {
		raw   string
		shape uint8
	}{
		{raw: "value=text", shape: 0},
		{raw: "value=one,two", shape: 1},
		{raw: "key=one&other=two", shape: 2},
		{raw: "value=%GG", shape: 0},
	} {
		f.Add(seed.raw, seed.shape)
	}
	version, err := specversion.Parse("3.2.0")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string, shapeIndex uint8) {
		shapes := []parameter.Shape{parameter.Primitive, parameter.Array, parameter.Object}
		shape := shapes[int(shapeIndex)%len(shapes)]
		value, err := parameter.Decode("value", raw, shape, parameter.Options{
			Version: version, Location: parameter.Query, Style: parameter.Form,
			Limits: parameter.Limits{MaxBytes: 4096, MaxItems: 256},
		})
		if err != nil {
			return
		}
		if _, err := value.MarshalJSON(); err != nil {
			t.Fatalf("decoded semantic value failed to marshal: %v", err)
		}
	})
}

func FuzzSwagger20QueryDecode(f *testing.F) {
	for _, seed := range []string{"value=one,two", "value=one%2Ctwo", "wrong=one", "value=%GG"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		value, err := parameter.DecodeSwagger20(
			"value",
			raw,
			parameter.Array,
			parameter.Swagger20Options{
				Location: parameter.Query,
				Limits:   parameter.Limits{MaxBytes: 4096, MaxItems: 256},
			},
		)
		if err != nil {
			return
		}
		if _, err := value.MarshalJSON(); err != nil {
			t.Fatalf("decoded semantic value failed to marshal: %v", err)
		}
	})
}
