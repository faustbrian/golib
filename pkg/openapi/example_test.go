package openapi_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/compose"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func ExampleParseJSON() {
	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(`{
			"openapi":"3.2.0",
			"info":{"title":"Example","version":"1"},
			"paths":{}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		panic(err)
	}
	fmt.Println(document.SpecificationVersion(), report.Valid())
	// Output: 3.2.0 true
}

func ExampleParseYAML() {
	document, err := openapi.ParseYAML(
		context.Background(),
		strings.NewReader(`
openapi: 3.1.2
info:
  title: Example
  version: "1"
paths: {}
`),
		parse.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}
	options := serialize.DefaultOptions()
	options.Mode = serialize.Canonical
	var output bytes.Buffer
	if err := serialize.JSON(
		context.Background(), &output, document, options,
	); err != nil {
		panic(err)
	}
	fmt.Println(output.String())
	// Output: {"info":{"title":"Example","version":"1"},"openapi":"3.1.2","paths":{}}
}

func ExampleMerge() {
	first, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(`{
			"openapi":"3.2.0","paths":{},
			"components":{"schemas":{"Pet":{"type":"string"}}}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}
	second, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(`{
			"openapi":"3.2.0","paths":{},
			"components":{"schemas":{"Pet":{"type":"integer"}}}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}
	options := compose.DefaultMergeOptions()
	options.ResolveConflict = func(compose.Conflict) (compose.ConflictDecision, error) {
		return compose.RenameIncoming, nil
	}
	result, err := compose.Merge(
		context.Background(), []openapi.Document{first, second}, options,
	)
	if err != nil {
		panic(err)
	}
	rename := result.Contributions()[0]
	fmt.Println(rename.SourcePointer(), "->", rename.TargetPointer())
	// Output: /components/schemas/Pet -> /components/schemas/Pet_2
}
