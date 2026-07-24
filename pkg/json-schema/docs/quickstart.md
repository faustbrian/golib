# Quick start

Install the module using the version selected by your application's dependency
policy:

```sh
go get github.com/faustbrian/golib/pkg/json-schema
```

Compile once and validate many times:

```go
package main

import (
	"context"
	"fmt"
	"log"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func main() {
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft202012),
	)
	if err != nil {
		log.Fatal(err)
	}

	schema, err := compiler.Compile(context.Background(), []byte(`{
		"type": "object",
		"required": ["name"],
		"properties": {"name": {"type": "string"}}
	}`))
	if err != nil {
		log.Fatal(err)
	}

	result, err := schema.Validate(
		context.Background(),
		[]byte(`{"name":"Ada"}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Valid)
}
```

Schema mismatch is reported as `Valid == false`. A non-nil error means input,
resource, cancellation, extension, or limit processing prevented a decision.

Next, choose a [dialect](dialects.md), configure [resource loading](resolvers.md)
when `$ref` crosses documents, and select [output](output.md) when callers need
machine-readable diagnostics.
