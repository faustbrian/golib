package jsonschema_test

import (
	"context"
	"encoding/json"
	"fmt"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func Example() {
	compiler, _ := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft202012),
	)
	schema, _ := compiler.Compile(
		context.Background(),
		[]byte(`{"type":"integer","minimum":1}`),
	)

	result, _ := schema.Validate(context.Background(), []byte(`2`))
	fmt.Println(result.Valid)

	// Output: true
}

func ExampleSchema_ValidateValue() {
	compiler, _ := jsonschema.NewCompiler()
	schema, _ := compiler.Compile(
		context.Background(),
		[]byte(`{"type":"number","multipleOf":0.1}`),
	)

	result, _ := schema.ValidateValue(context.Background(), json.Number("0.3"))
	fmt.Println(result.Valid)

	// Output: true
}

func ExampleMapLoader() {
	loader, _ := jsonschema.NewMapLoader(map[string][]byte{
		"https://schemas.example.test/name": []byte(`{
			"$id":"https://schemas.example.test/name",
			"type":"string",
			"minLength":1
		}`),
	})
	compiler, _ := jsonschema.NewCompiler(jsonschema.WithResourceLoader(loader))
	schema, _ := compiler.Compile(
		context.Background(),
		[]byte(`{"$ref":"https://schemas.example.test/name"}`),
	)

	result, _ := schema.Validate(context.Background(), []byte(`"Ada"`))
	fmt.Println(result.Valid)

	// Output: true
}

func ExampleSchema_ValidateOutput() {
	compiler, _ := jsonschema.NewCompiler()
	schema, _ := compiler.Compile(context.Background(), []byte(`{"type":"string"}`))

	output, _ := schema.ValidateOutput(
		context.Background(),
		[]byte(`42`),
		jsonschema.OutputFlag,
	)
	encoded, _ := json.Marshal(output)
	fmt.Println(string(encoded))

	// Output: {"valid":false}
}
