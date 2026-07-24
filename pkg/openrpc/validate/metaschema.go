package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrMetaSchemaPolicy reports a non-positive structural diagnostic bound.
	ErrMetaSchemaPolicy = errors.New("validate: invalid meta-schema policy")
	// ErrMetaSchemaUnavailable reports failure to parse or compile the pinned
	// authoritative schema.
	ErrMetaSchemaUnavailable = errors.New("validate: OpenRPC meta-schema unavailable")

	metaSchemaOnce      sync.Once
	metaSchemaValidator jsonschema.Validator
	metaSchemaError     error
)

// MetaSchemaReport is a bounded structural validation result.
type MetaSchemaReport struct {
	report jsonschema.Report
	err    error
}

// Issues returns an owned structural diagnostic snapshot.
func (report MetaSchemaReport) Issues() []jsonschema.Issue { return report.report.Issues() }

// Truncated reports that the diagnostic bound omitted additional failures.
func (report MetaSchemaReport) Truncated() bool { return report.report.Truncated() }

// Err reports policy, compilation, cancellation, or instance failures.
func (report MetaSchemaReport) Err() error {
	if report.err != nil {
		return report.err
	}
	return report.report.Err()
}

// Valid reports successful structural validation.
func (report MetaSchemaReport) Valid() bool {
	return report.err == nil && report.report.Valid()
}

// MetaSchema validates raw JSON against the pinned OpenRPC 1.4.1 meta-schema.
// Compilation installs no external loader and performs no implicit I/O.
func MetaSchema(ctx context.Context, document jsonvalue.Value, maxIssues int) MetaSchemaReport {
	if ctx == nil || maxIssues <= 0 {
		return MetaSchemaReport{err: ErrMetaSchemaPolicy}
	}
	if err := ctx.Err(); err != nil {
		return MetaSchemaReport{err: err}
	}
	metaSchemaOnce.Do(compileMetaSchema)
	if metaSchemaError != nil {
		return MetaSchemaReport{err: ErrMetaSchemaUnavailable}
	}
	compiled, _ := metaSchemaValidator.WithMaxIssues(maxIssues)
	return MetaSchemaReport{report: compiled.Validate(ctx, document)}
}

func compileMetaSchema() {
	metaSchemaValidator, metaSchemaError = compileMetaSchemaBytes(
		openrpc.MetaSchema(), openrpc.JSONSchemaToolsMetaSchema(),
	)
}

func compileMetaSchemaBytes(schemaInput []byte, companionInput []byte) (jsonschema.Validator, error) {
	schemaBytes, err := draft7CompatibleMetaSchema(schemaInput)
	if err != nil {
		return jsonschema.Validator{}, err
	}
	companionBytes, err := draft7CompatibleMetaSchema(companionInput)
	if err != nil {
		return jsonschema.Validator{}, err
	}
	schema, err := jsonschema.Parse(schemaBytes, jsonvalue.DefaultPolicy())
	if err != nil {
		return jsonschema.Validator{}, err
	}
	companion, err := jsonschema.Parse(companionBytes, jsonvalue.DefaultPolicy())
	if err != nil {
		return jsonschema.Validator{}, err
	}
	options := jsonschema.DefaultValidationOptions()
	options.Resources = map[string]jsonschema.Schema{
		"https://meta.json-schema.tools/": companion,
	}
	return jsonschema.Compile(schema, options)
}

func draft7CompatibleMetaSchema(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	rewriteMetaDialect(document)
	alignServerURLWithNormativeSemantics(document)
	encoded, _ := json.Marshal(document)
	return encoded, nil
}

// OpenRPC permits relative server URLs and server-variable templates. The
// pinned schema's generic URI format rejects both, contradicting the normative
// Server Object description, so structural validation leaves this field to
// the package's dedicated server-expression validation.
func alignServerURLWithNormativeSemantics(document any) {
	root, ok := document.(map[string]any)
	if !ok {
		return
	}
	definitions, ok := root["definitions"].(map[string]any)
	if !ok {
		return
	}
	server, ok := definitions["serverObject"].(map[string]any)
	if !ok {
		return
	}
	properties, ok := server["properties"].(map[string]any)
	if !ok {
		return
	}
	urlSchema, ok := properties["url"].(map[string]any)
	if !ok {
		return
	}
	delete(urlSchema, "format")
}

func rewriteMetaDialect(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if dialect, ok := typed["$schema"].(string); ok &&
			strings.TrimSuffix(dialect, "/") == "https://meta.json-schema.tools" {
			typed["$schema"] = "http://json-schema.org/draft-07/schema#"
		}
		if reference, ok := typed["$ref"].(string); ok &&
			strings.HasPrefix(reference, "https://meta.json-schema.tools") {
			suffix := strings.TrimPrefix(reference, "https://meta.json-schema.tools")
			typed["$ref"] = "https://meta.json-schema.tools/" + strings.TrimPrefix(suffix, "/")
		}
		for _, child := range typed {
			rewriteMetaDialect(child)
		}
	case []any:
		for _, child := range typed {
			rewriteMetaDialect(child)
		}
	}
}
