package ecmascript

import "context"

// JSONSchemaPattern is an immutable JSON Schema Draft 2020-12 pattern.
// JSON Schema patterns are processed in ECMAScript Unicode mode and are not
// implicitly anchored.
type JSONSchemaPattern struct {
	program *Program
}

// CompileJSONSchemaPattern compiles a JSON Schema pattern with ECMAScript
// Unicode semantics. JSON Schema has no syntax for flags, so this profile
// selects only the u flag and leaves anchoring entirely to the pattern.
func CompileJSONSchemaPattern(source string, options CompileOptions) (*JSONSchemaPattern, error) {
	program, err := Compile(source, "u", options)
	if err != nil {
		return nil, err
	}

	return &JSONSchemaPattern{program: program}, nil
}

// Match reports whether the pattern matches anywhere in the JSON string.
// Execution remains subject to the caller-provided resource budgets.
func (p *JSONSchemaPattern) Match(ctx context.Context, input string, options MatchOptions) (bool, error) {
	_, matched, err := p.program.Find(ctx, input, options)
	return matched, err
}

// Program exposes the immutable compiled program for callers that need match
// spans or captures in addition to JSON Schema's boolean assertion result.
func (p *JSONSchemaPattern) Program() *Program {
	return p.program
}
