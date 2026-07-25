package jsonschema_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func FuzzSchemaObjectCompilationAndEvaluation(f *testing.F) {
	for _, seed := range []struct {
		schema   string
		instance string
		dialect  uint8
	}{
		{schema: `{}`, instance: `null`, dialect: 2},
		{schema: `false`, instance: `"value"`, dialect: 3},
		{schema: `{"type":"string"}`, instance: `"value"`, dialect: 0},
		{schema: `{"nullable":true,"type":"string"}`, instance: `null`, dialect: 1},
	} {
		f.Add([]byte(seed.schema), []byte(seed.instance), seed.dialect)
	}
	f.Fuzz(func(t *testing.T, schemaRaw []byte, instanceRaw []byte, dialectIndex uint8) {
		limits := fuzzParseLimits()
		schema, err := parse.JSON(context.Background(), bytes.NewReader(schemaRaw), limits)
		if err != nil {
			return
		}
		dialects := []jsonschema.Dialect{
			jsonschema.DialectSwagger20,
			jsonschema.DialectOAS30,
			jsonschema.DialectOAS31,
			jsonschema.DialectOAS32,
		}
		compiler, err := jsonschema.NewCompiler(
			dialects[int(dialectIndex)%len(dialects)],
			jsonschema.WithTraversalLimits(2_048, 64),
		)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile(context.Background(), schema)
		if err != nil {
			return
		}
		_, _ = compiled.Validate(context.Background(), instanceRaw)
	})
}

func fuzzParseLimits() parse.Limits {
	limits := parse.DefaultLimits()
	limits.MaxBytes = 32 << 10
	limits.MaxTokens = 4_096
	limits.MaxDepth = 64
	limits.MaxObjectMembers = 1_024
	limits.MaxArrayItems = 1_024
	limits.MaxScalarBytes = 8 << 10
	limits.MaxTotalValues = 2_048
	return limits
}
