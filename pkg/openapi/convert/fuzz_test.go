package convert_test

import (
	"bytes"
	"context"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/convert"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func FuzzConvertPatchAndForwardVersions(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.0.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.1.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		document, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), parse.DefaultLimits(),
		)
		if err != nil {
			return
		}
		targetText := document.SpecificationVersion().String()
		switch document.SpecificationVersion().Dialect() {
		case openapi.DialectSwagger20:
			targetText = "3.0.4"
		case openapi.DialectOAS30:
			targetText = "3.1.2"
		case openapi.DialectOAS31:
			targetText = "3.2.0"
		case openapi.DialectOAS32:
			targetText = "3.1.2"
		}
		target, _ := openapi.ParseVersion(targetText)
		result, err := convert.To(
			context.Background(), document, target, convert.DefaultOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Document().SpecificationVersion().String() != targetText {
			t.Fatalf(
				"converted version = %s, want %s",
				result.Document().SpecificationVersion(),
				targetText,
			)
		}
	})
}

func FuzzConvertOpenAPI31To30(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.1.2","info":{"title":"API","version":"1"},` +
			`"paths":{},"components":{"schemas":{"Value":` +
			`{"type":["string","null"],"const":"x"}}}}`,
		`{"openapi":"3.1.2","info":{"title":"API","version":"1"},` +
			`"paths":{},"components":{"schemas":{"Never":false}}}`,
	} {
		f.Add([]byte(seed))
	}
	target, _ := openapi.ParseVersion("3.0.4")
	f.Fuzz(func(t *testing.T, raw []byte) {
		document, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), parse.DefaultLimits(),
		)
		if err != nil ||
			document.SpecificationVersion().Dialect() != openapi.DialectOAS31 {
			return
		}
		result, err := convert.To(
			context.Background(), document, target, convert.DefaultOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Document().SpecificationVersion().String() != "3.0.4" {
			t.Fatalf("converted version = %s", result.Document().SpecificationVersion())
		}
	})
}

func FuzzConvertOpenAPI30ToSwagger20(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.0.4","info":{"title":"API","version":"1"},` +
			`"paths":{},"components":{"schemas":{"Value":{"type":"string"}}}}`,
		`{"openapi":"3.0.4","info":{"title":"API","version":"1"},` +
			`"paths":{"/items":{"post":{"requestBody":{"content":{` +
			`"application/json":{"schema":{"type":"object"}}}},` +
			`"responses":{"204":{"description":"OK"}}}}}}`,
	} {
		f.Add([]byte(seed))
	}
	target, _ := openapi.ParseVersion("2.0")
	f.Fuzz(func(t *testing.T, raw []byte) {
		document, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), parse.DefaultLimits(),
		)
		if err != nil ||
			document.SpecificationVersion().Dialect() != openapi.DialectOAS30 {
			return
		}
		sourceReport, err := validate.Document(context.Background(), document)
		if err != nil || !sourceReport.Valid() {
			return
		}
		result, err := convert.To(
			context.Background(), document, target, convert.DefaultOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Document().SpecificationVersion().String() != "2.0" {
			t.Fatalf("converted version = %s", result.Document().SpecificationVersion())
		}
		targetReport, err := validate.Document(
			context.Background(), result.Document(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if !targetReport.Valid() {
			t.Fatalf("invalid converted document: %#v", targetReport.Diagnostics())
		}
	})
}
