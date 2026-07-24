package jsonschema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	// ErrValidationPolicy reports invalid compilation or diagnostic bounds.
	ErrValidationPolicy = errors.New("jsonschema: invalid validation policy")
	// ErrSchemaCompile reports a schema that is not valid Draft 7 or contains
	// a reference absent from the explicit resource set.
	ErrSchemaCompile = errors.New("jsonschema: Draft 7 compilation failed")
	// ErrInvalidInstance reports an invalid zero instance value.
	ErrInvalidInstance = errors.New("jsonschema: invalid instance")
)

const defaultBaseURI = "https://openrpc.invalid/schema.json"

// ValidationOptions configures Draft 7 compilation and bounded reporting.
// Resources are the only external schemas available during compilation; no
// filesystem or network loader is installed.
type ValidationOptions struct {
	BaseURI        string
	Resources      map[string]Schema
	MaxResources   int
	MaxSchemaBytes int
	MaxIssues      int
	RegexpTimeout  time.Duration
}

// DefaultValidationOptions returns strict Draft 7 compilation with finite
// diagnostic output and no external resources.
func DefaultValidationOptions() ValidationOptions {
	return ValidationOptions{
		BaseURI: defaultBaseURI, MaxResources: 1_024,
		MaxSchemaBytes: 64 << 20, MaxIssues: 1_000,
		RegexpTimeout: 100 * time.Millisecond,
	}
}

// Validator is an immutable compiled Draft 7 schema safe for concurrent use.
type Validator struct {
	compiled  *validator.Schema
	maxIssues int
}

// WithMaxIssues returns a validator sharing the immutable compiled schema with
// a replacement diagnostic bound.
func (compiled Validator) WithMaxIssues(maxIssues int) (Validator, error) {
	if compiled.compiled == nil || maxIssues <= 0 {
		return Validator{}, ErrValidationPolicy
	}
	return Validator{compiled: compiled.compiled, maxIssues: maxIssues}, nil
}

// Compile compiles one Draft 7 schema using only explicitly supplied
// resources. It never performs network or filesystem access.
func Compile(schema Schema, options ValidationOptions) (Validator, error) {
	if options.MaxResources <= 0 || options.MaxIssues <= 0 ||
		!absoluteURI(options.BaseURI) ||
		options.RegexpTimeout <= 0 || options.RegexpTimeout > 10*time.Second {
		return Validator{}, ErrValidationPolicy
	}
	if len(options.Resources) > options.MaxResources {
		return Validator{}, ErrValidationPolicy
	}
	schemaBytes := schema.Bytes()
	if len(schemaBytes) > options.MaxSchemaBytes {
		return Validator{}, ErrValidationPolicy
	}
	if !declaresDraft7(schemaBytes) {
		return Validator{}, ErrSchemaCompile
	}
	// Schema guarantees syntactically valid object or boolean JSON.
	document, _ := validator.UnmarshalJSON(bytes.NewReader(schemaBytes))
	compiler := validator.NewCompiler()
	compiler.DefaultDraft(validator.Draft7)
	compiler.AssertFormat()
	compiler.UseRegexpEngine(ecmaRegexpEngine(options.RegexpTimeout))

	resourceNames := make([]string, 0, len(options.Resources))
	for resourceURI := range options.Resources {
		resourceNames = append(resourceNames, resourceURI)
	}
	sort.Strings(resourceNames)
	totalSchemaBytes := len(schemaBytes)
	for _, resourceURI := range resourceNames {
		if !absoluteURI(resourceURI) {
			return Validator{}, ErrValidationPolicy
		}
		resource := options.Resources[resourceURI]
		resourceBytes := resource.Bytes()
		if len(resourceBytes) > options.MaxSchemaBytes-totalSchemaBytes {
			return Validator{}, ErrValidationPolicy
		}
		totalSchemaBytes += len(resourceBytes)
		if !declaresDraft7(resourceBytes) {
			return Validator{}, ErrSchemaCompile
		}
		// Resource schemas share the same syntax invariant, and resource names
		// are unique absolute map keys.
		decoded, _ := validator.UnmarshalJSON(bytes.NewReader(resourceBytes))
		_ = compiler.AddResource(resourceURI, decoded)
	}
	if err := compiler.AddResource(options.BaseURI, document); err != nil {
		return Validator{}, ErrSchemaCompile
	}
	compiled, err := compiler.Compile(options.BaseURI)
	if err != nil || compiled.DraftVersion != 7 {
		return Validator{}, ErrSchemaCompile
	}
	return Validator{compiled: compiled, maxIssues: options.MaxIssues}, nil
}

type ecmaRegexp struct {
	compiled *regexp2.Regexp
}

func (expression ecmaRegexp) MatchString(input string) bool {
	matched, err := expression.compiled.MatchString(input)
	return err == nil && matched
}

func (expression ecmaRegexp) String() string { return expression.compiled.String() }

func ecmaRegexpEngine(timeout time.Duration) validator.RegexpEngine {
	return func(pattern string) (validator.Regexp, error) {
		compiled, err := regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		compiled.MatchTimeout = timeout
		return ecmaRegexp{compiled: compiled}, nil
	}
}

// Issue is one safe validation failure. Messages contain no instance value.
type Issue struct {
	InstancePointer string
	SchemaPointer   string
	Keyword         string
	Message         string
}

// Report is one immutable bounded validation result.
type Report struct {
	issues    []Issue
	truncated bool
	err       error
}

// Issues returns an owned diagnostic snapshot.
func (report Report) Issues() []Issue { return append([]Issue(nil), report.issues...) }

// Truncated reports that more failures existed than the configured bound.
func (report Report) Truncated() bool { return report.truncated }

// Err reports cancellation, invalid policy, or an invalid instance.
func (report Report) Err() error { return report.err }

// Valid reports successful validation and no execution error.
func (report Report) Valid() bool { return report.err == nil && len(report.issues) == 0 }

// Validate checks one immutable JSON value and converts dependency diagnostics
// into stable, payload-free package-owned issues.
func (compiled Validator) Validate(ctx context.Context, instance jsonvalue.Value) Report {
	if compiled.compiled == nil || compiled.maxIssues <= 0 || ctx == nil {
		return Report{err: ErrValidationPolicy}
	}
	if err := ctx.Err(); err != nil {
		return Report{err: err}
	}
	value, err := validator.UnmarshalJSON(bytes.NewReader(instance.Bytes()))
	if err != nil {
		return Report{err: ErrInvalidInstance}
	}
	err = compiled.compiled.Validate(value)
	if err == nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Report{err: contextErr}
		}
		return Report{}
	}
	// A compiled schema returns only nil or *ValidationError for a decoded
	// instance; decoding failures were handled above.
	var validationError *validator.ValidationError
	_ = errors.As(err, &validationError)
	sortValidationErrors(validationError)
	issues := make([]Issue, 0, min(compiled.maxIssues, 8))
	total := collectIssues(validationError, compiled.maxIssues+1, &issues)
	truncated := total > compiled.maxIssues
	if truncated {
		issues = issues[:compiled.maxIssues]
	}
	return Report{issues: issues, truncated: truncated}
}

func sortValidationErrors(current *validator.ValidationError) {
	for _, cause := range current.Causes {
		sortValidationErrors(cause)
	}
	sort.SliceStable(current.Causes, func(left int, right int) bool {
		return strings.Compare(
			validationErrorKey(current.Causes[left]),
			validationErrorKey(current.Causes[right]),
		) == -1
	})
}

func validationErrorKey(current *validator.ValidationError) string {
	for current != nil && len(current.Causes) != 0 {
		current = current.Causes[0]
	}
	if current == nil {
		return ""
	}
	return jsonPointer(current.InstanceLocation) + "\x00" +
		jsonPointer(current.ErrorKind.KeywordPath())
}

func collectIssues(current *validator.ValidationError, limit int, issues *[]Issue) int {
	if current == nil {
		return 0
	}
	if len(current.Causes) != 0 {
		total := 0
		for _, cause := range current.Causes {
			if len(*issues) >= limit {
				return limit
			}
			total += collectIssues(cause, limit, issues)
		}
		return total
	}
	keywordPath := current.ErrorKind.KeywordPath()
	keyword := "false"
	if len(keywordPath) != 0 {
		keyword = keywordPath[len(keywordPath)-1]
	}
	*issues = append(*issues, Issue{
		InstancePointer: jsonPointer(current.InstanceLocation),
		SchemaPointer:   jsonPointer(keywordPath),
		Keyword:         keyword,
		Message:         "value does not satisfy the schema keyword",
	})
	return 1
}

func jsonPointer(segments []string) string {
	if len(segments) == 0 {
		return "#"
	}
	escaped := make([]string, len(segments))
	for index, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		escaped[index] = strings.ReplaceAll(segment, "/", "~1")
	}
	return "#/" + strings.Join(escaped, "/")
}

func declaresDraft7(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")) {
		return true
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return false
	}
	raw, exists := object["$schema"]
	if !exists {
		return true
	}
	var dialect string
	if err := json.Unmarshal(raw, &dialect); err != nil {
		return false
	}
	switch dialect {
	case "http://json-schema.org/draft-07/schema#",
		"http://json-schema.org/draft-07/schema",
		"https://json-schema.org/draft-07/schema#",
		"https://json-schema.org/draft-07/schema":
		return true
	default:
		return false
	}
}

func absoluteURI(input string) bool {
	parsed, err := url.Parse(input)
	return err == nil && parsed.IsAbs() && parsed.Fragment == ""
}
