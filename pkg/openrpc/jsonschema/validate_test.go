package jsonschema_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestValidatorAppliesDraft7WithoutNumericCoercion(t *testing.T) {
	t.Parallel()

	schema := parseSchema(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"object",
		"required":["name","count"],
		"properties":{
			"name":{"type":"string","minLength":3},
			"count":{"type":"integer","minimum":9007199254740993}
		}
	}`)
	validator, err := jsonschema.Compile(schema, jsonschema.DefaultValidationOptions())
	if err != nil {
		t.Fatal(err)
	}
	valid := parseValue(t, `{"name":"valid","count":9007199254740993}`)
	if report := validator.Validate(context.Background(), valid); !report.Valid() {
		t.Fatalf("valid report = %#v", report.Issues())
	}
	invalid := parseValue(t, `{"name":"x","count":9007199254740992}`)
	report := validator.Validate(context.Background(), invalid)
	if report.Valid() || len(report.Issues()) != 2 {
		t.Fatalf("invalid report = %#v", report.Issues())
	}
	for _, issue := range report.Issues() {
		if issue.InstancePointer == "" || issue.SchemaPointer == "" || issue.Keyword == "" || issue.Message == "" {
			t.Fatalf("incomplete issue = %#v", issue)
		}
	}
	if report.Issues()[0].InstancePointer != "#/count" ||
		report.Issues()[1].InstancePointer != "#/name" {
		t.Fatalf("issues are not deterministic: %#v", report.Issues())
	}
	returned := report.Issues()
	returned[0].Keyword = "changed"
	if report.Issues()[0].Keyword == "changed" {
		t.Fatal("Issues exposed mutable report storage")
	}
}

func TestValidatorSupportsBooleanSchemas(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		schema string
		valid  bool
	}{
		{schema: "true", valid: true},
		{schema: "false", valid: false},
	} {
		validator, err := jsonschema.Compile(parseSchema(t, test.schema), jsonschema.DefaultValidationOptions())
		if err != nil {
			t.Fatal(err)
		}
		if got := validator.Validate(context.Background(), parseValue(t, `null`)).Valid(); got != test.valid {
			t.Errorf("schema %s valid = %t", test.schema, got)
		}
	}
}

func TestCompileRejectsInvalidOrNonDraft7Schemas(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`{"maxLength":"not-an-integer"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema"}`,
		`{"$ref":"https://example.com/external.json"}`,
	} {
		_, err := jsonschema.Compile(parseSchema(t, source), jsonschema.DefaultValidationOptions())
		if !errors.Is(err, jsonschema.ErrSchemaCompile) {
			t.Errorf("Compile(%s) error = %v", source, err)
		}
	}
}

func TestCompileEnforcesExactOptionBoundaries(t *testing.T) {
	t.Parallel()

	for _, mutate := range []func(*jsonschema.ValidationOptions){
		func(options *jsonschema.ValidationOptions) { options.MaxResources = 0 },
		func(options *jsonschema.ValidationOptions) { options.MaxSchemaBytes = 0 },
		func(options *jsonschema.ValidationOptions) { options.MaxIssues = 0 },
		func(options *jsonschema.ValidationOptions) { options.RegexpTimeout = 0 },
		func(options *jsonschema.ValidationOptions) { options.RegexpTimeout = 10*time.Second + 1 },
	} {
		options := jsonschema.DefaultValidationOptions()
		mutate(&options)
		if _, err := jsonschema.Compile(parseSchema(t, `true`), options); !errors.Is(err, jsonschema.ErrValidationPolicy) {
			t.Fatalf("Compile options %#v error = %v", options, err)
		}
	}
	options := jsonschema.DefaultValidationOptions()
	options.RegexpTimeout = 10 * time.Second
	if _, err := jsonschema.Compile(parseSchema(t, `true`), options); err != nil {
		t.Fatalf("exact timeout boundary error = %v", err)
	}
}

func TestCompileUsesOnlyExplicitExternalResources(t *testing.T) {
	t.Parallel()

	options := jsonschema.DefaultValidationOptions()
	options.Resources = map[string]jsonschema.Schema{
		"https://example.com/positive.json": parseSchema(t, `{"type":"integer","minimum":1}`),
	}
	validator, err := jsonschema.Compile(
		parseSchema(t, `{"$ref":"https://example.com/positive.json"}`),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if validator.Validate(context.Background(), parseValue(t, `0`)).Valid() {
		t.Fatal("explicit external schema was not applied")
	}
	options.Resources["https://example.com/positive.json"] = parseSchema(t, `true`)
	if validator.Validate(context.Background(), parseValue(t, `0`)).Valid() {
		t.Fatal("compiler retained caller-owned resource map")
	}
}

func TestCompileBoundsExplicitSchemaResources(t *testing.T) {
	t.Parallel()

	root := parseSchema(t, `true`)
	resource := parseSchema(t, `{"type":"string"}`)
	rootOptions := jsonschema.DefaultValidationOptions()
	rootOptions.MaxSchemaBytes = len(root.Bytes())
	if _, err := jsonschema.Compile(root, rootOptions); err != nil {
		t.Fatalf("exact root schema byte limit failed: %v", err)
	}
	rootOptions.MaxSchemaBytes--
	if _, err := jsonschema.Compile(root, rootOptions); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("root schema byte error = %v", err)
	}

	options := jsonschema.DefaultValidationOptions()
	options.MaxResources = 1
	options.Resources = map[string]jsonschema.Schema{
		"https://example.com/one.json": resource,
	}
	options.MaxSchemaBytes = len(root.Bytes()) + len(resource.Bytes())
	if _, err := jsonschema.Compile(root, options); err != nil {
		t.Fatalf("exact resource limits failed: %v", err)
	}

	options.Resources["https://example.com/two.json"] = resource
	if _, err := jsonschema.Compile(root, options); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("resource count error = %v", err)
	}
	delete(options.Resources, "https://example.com/two.json")
	options.MaxSchemaBytes--
	if _, err := jsonschema.Compile(root, options); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("aggregate schema byte error = %v", err)
	}
}

func TestCompileRejectsInvalidResourcePolicies(t *testing.T) {
	t.Parallel()

	tests := []jsonschema.ValidationOptions{
		func() jsonschema.ValidationOptions {
			options := jsonschema.DefaultValidationOptions()
			options.Resources = map[string]jsonschema.Schema{"relative.json": parseSchema(t, `true`)}
			return options
		}(),
		func() jsonschema.ValidationOptions {
			options := jsonschema.DefaultValidationOptions()
			options.Resources = map[string]jsonschema.Schema{
				"https://example.com/future.json": parseSchema(t, `{"$schema":"https://json-schema.org/draft/2020-12/schema"}`),
			}
			return options
		}(),
	}
	for _, options := range tests {
		if _, err := jsonschema.Compile(parseSchema(t, `true`), options); err == nil {
			t.Fatalf("Compile options %#v succeeded", options)
		}
	}
	duplicate := jsonschema.DefaultValidationOptions()
	duplicate.BaseURI = "https://example.com/schema.json"
	duplicate.Resources = map[string]jsonschema.Schema{duplicate.BaseURI: parseSchema(t, `true`)}
	if _, err := jsonschema.Compile(parseSchema(t, `true`), duplicate); !errors.Is(err, jsonschema.ErrSchemaCompile) {
		t.Fatalf("duplicate base error = %v", err)
	}
}

func TestValidatorUsesBoundedECMAScriptRegularExpressions(t *testing.T) {
	t.Parallel()

	schema := parseSchema(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"string",
		"pattern":"^\\cc$"
	}`)
	compiled, err := jsonschema.Compile(schema, jsonschema.DefaultValidationOptions())
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonvalue.Parse([]byte(`"\u0003"`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if report := compiled.Validate(context.Background(), instance); !report.Valid() {
		t.Fatalf("issues = %#v, error = %v", report.Issues(), report.Err())
	}
}

func TestValidationBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	if _, err := jsonschema.Compile(parseSchema(t, `true`), jsonschema.ValidationOptions{}); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("policy error = %v", err)
	}
	options := jsonschema.DefaultValidationOptions()
	options.MaxIssues = 1
	validator, err := jsonschema.Compile(
		parseSchema(t, `{"type":"array","items":{"type":"string"}}`),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	report := validator.Validate(context.Background(), parseValue(t, `[1,2,3]`))
	if len(report.Issues()) != 1 || !report.Truncated() {
		t.Fatalf("bounded report = %#v, truncated = %t", report.Issues(), report.Truncated())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report = validator.Validate(ctx, parseValue(t, `[]`))
	if !errors.Is(report.Err(), context.Canceled) {
		t.Fatalf("canceled report error = %v", report.Err())
	}
	bounded, err := validator.WithMaxIssues(3)
	if err != nil {
		t.Fatal(err)
	}
	if bounded.Validate(context.Background(), parseValue(t, `[1,2,3]`)).Truncated() == true {
		t.Fatal("WithMaxIssues did not apply the replacement bound")
	}
	if _, err := validator.WithMaxIssues(0); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("WithMaxIssues error = %v", err)
	}
}

func TestValidateRejectsInvalidExecutionInputs(t *testing.T) {
	t.Parallel()

	var zero jsonschema.Validator
	if report := zero.Validate(context.Background(), parseValue(t, `null`)); !errors.Is(report.Err(), jsonschema.ErrValidationPolicy) {
		t.Fatalf("zero validator error = %v", report.Err())
	}
	if _, err := zero.WithMaxIssues(1); !errors.Is(err, jsonschema.ErrValidationPolicy) {
		t.Fatalf("zero WithMaxIssues error = %v", err)
	}
	compiled, err := jsonschema.Compile(parseSchema(t, `true`), jsonschema.DefaultValidationOptions())
	if err != nil {
		t.Fatal(err)
	}
	var invalidContext context.Context
	if report := compiled.Validate(invalidContext, parseValue(t, `null`)); !errors.Is(report.Err(), jsonschema.ErrValidationPolicy) {
		t.Fatalf("nil context error = %v", report.Err())
	}
	if report := compiled.Validate(context.Background(), jsonvalue.Value{}); !errors.Is(report.Err(), jsonschema.ErrInvalidInstance) {
		t.Fatalf("zero instance error = %v", report.Err())
	}
	ctx := &secondCheckCanceledContext{}
	if report := compiled.Validate(ctx, parseValue(t, `null`)); !errors.Is(report.Err(), context.Canceled) {
		t.Fatalf("post-validation context error = %v", report.Err())
	}
}

type secondCheckCanceledContext struct{ checks int }

func (ctx *secondCheckCanceledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *secondCheckCanceledContext) Done() <-chan struct{}       { return nil }
func (ctx *secondCheckCanceledContext) Value(any) any               { return nil }
func (ctx *secondCheckCanceledContext) Err() error {
	ctx.checks++
	if ctx.checks > 1 {
		return context.Canceled
	}
	return nil
}

func parseSchema(t *testing.T, input string) jsonschema.Schema {
	t.Helper()
	schema, err := jsonschema.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func parseValue(t *testing.T, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
