// Package server expands OpenAPI Server Object URL templates without owning
// HTTP transport state or applying implicit escaping.
package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrInvalidServer reports a malformed Server Object or substitution value.
var ErrInvalidServer = errors.New("invalid OpenAPI server")

// ErrInvalidTemplate reports malformed or nested variable braces.
var ErrInvalidTemplate = errors.New("invalid OpenAPI server URL template")

// ErrMissingVariable reports a URL variable without a declaration.
var ErrMissingVariable = errors.New("missing OpenAPI server variable")

// ErrUnusedOverride reports a caller override absent from the URL template.
var ErrUnusedOverride = errors.New("unused OpenAPI server variable override")

// ErrInvalidOptions reports negative expansion limits.
var ErrInvalidOptions = errors.New("invalid OpenAPI server expansion options")

// ErrLimitExceeded reports output or variable growth beyond caller policy.
var ErrLimitExceeded = errors.New("OpenAPI server expansion limit exceeded")

// Options controls caller overrides and resource bounds. Values are inserted
// exactly as supplied; callers own any required percent-encoding or escaping.
type Options struct {
	Values         map[string]string
	MaxOutputBytes int
	MaxVariables   int
}

// DefaultOptions returns conservative bounds for runtime expansion.
func DefaultOptions() Options {
	return Options{MaxOutputBytes: 1 << 20, MaxVariables: 10_000}
}

// Expand substitutes declared variable defaults and caller overrides into one
// immutable Server Object. It never mutates caller maps or semantic values.
func Expand(value jsonvalue.Value, options Options) (string, error) {
	options, valid := effectiveOptions(options)
	if !valid {
		return "", ErrInvalidOptions
	}
	if value.Kind() != jsonvalue.ObjectKind {
		return "", ErrInvalidServer
	}
	urlValue, exists := value.Lookup("url")
	url, validURL := urlValue.Text()
	if !exists || !validURL {
		return "", fmt.Errorf("%w: URL must be a string", ErrInvalidServer)
	}
	defaults, err := variableDefaults(value, options.MaxVariables)
	if err != nil {
		return "", err
	}
	if len(options.Values) > options.MaxVariables {
		return "", fmt.Errorf("%w: overrides", ErrLimitExceeded)
	}
	for name, override := range options.Values {
		if !utf8.ValidString(name) || !utf8.ValidString(override) {
			return "", fmt.Errorf("%w: override is not valid UTF-8", ErrInvalidServer)
		}
	}

	result, used, err := expandTemplate(url, defaults, options)
	if err != nil {
		return "", err
	}
	var unused []string
	for name := range options.Values {
		if _, exists := used[name]; !exists {
			unused = append(unused, name)
		}
	}
	if len(unused) > 0 {
		sort.Strings(unused)
		return "", fmt.Errorf("%w: %s", ErrUnusedOverride, unused[0])
	}
	return result, nil
}

func effectiveOptions(options Options) (Options, bool) {
	if options.MaxOutputBytes < 0 || options.MaxVariables < 0 {
		return Options{}, false
	}
	defaults := DefaultOptions()
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if options.MaxVariables == 0 {
		options.MaxVariables = defaults.MaxVariables
	}
	return options, true
}

func variableDefaults(value jsonvalue.Value, maximum int) (map[string]string, error) {
	result := make(map[string]string)
	variables, exists := value.Lookup("variables")
	if !exists {
		return result, nil
	}
	if variables.Kind() != jsonvalue.ObjectKind {
		return nil, fmt.Errorf("%w: variables must be an object", ErrInvalidServer)
	}
	members, _ := variables.Members()
	if len(members) > maximum {
		return nil, fmt.Errorf("%w: declarations", ErrLimitExceeded)
	}
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind {
			return nil, fmt.Errorf("%w: variable must be an object", ErrInvalidServer)
		}
		defaultValue, exists := member.Value.Lookup("default")
		text, valid := defaultValue.Text()
		if !exists || !valid {
			return nil, fmt.Errorf("%w: variable default must be a string", ErrInvalidServer)
		}
		result[member.Name] = text
	}
	return result, nil
}

func expandTemplate(
	template string,
	defaults map[string]string,
	options Options,
) (string, map[string]struct{}, error) {
	used := make(map[string]struct{})
	var result strings.Builder
	remaining := template
	occurrences := 0
	for remaining != "" {
		firstBrace := strings.IndexAny(remaining, "{}")
		if firstBrace >= 0 && remaining[firstBrace] == '}' {
			return "", nil, ErrInvalidTemplate
		}
		opening := strings.IndexByte(remaining, '{')
		if opening < 0 {
			if err := appendBounded(&result, remaining, options.MaxOutputBytes); err != nil {
				return "", nil, err
			}
			break
		}
		if err := appendBounded(&result, remaining[:opening], options.MaxOutputBytes); err != nil {
			return "", nil, err
		}
		remaining = remaining[opening+1:]
		closing := strings.IndexByte(remaining, '}')
		if closing == -1 {
			return "", nil, ErrInvalidTemplate
		}
		name := remaining[:closing]
		if name == "" || strings.ContainsAny(name, "{}") {
			return "", nil, ErrInvalidTemplate
		}
		occurrences++
		if occurrences > options.MaxVariables {
			return "", nil, fmt.Errorf("%w: template variables", ErrLimitExceeded)
		}
		replacement, declared := defaults[name]
		if !declared {
			return "", nil, fmt.Errorf("%w: %s", ErrMissingVariable, name)
		}
		if override, exists := options.Values[name]; exists {
			replacement = override
		}
		if err := appendBounded(&result, replacement, options.MaxOutputBytes); err != nil {
			return "", nil, err
		}
		used[name] = struct{}{}
		remaining = remaining[closing+1:]
	}
	return result.String(), used, nil
}

func appendBounded(result *strings.Builder, value string, maximum int) error {
	if len(value) > maximum-result.Len() {
		return ErrLimitExceeded
	}
	result.WriteString(value)
	return nil
}
