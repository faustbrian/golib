package expression

import (
	"bytes"
	"encoding/json"
	"sort"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

// EvaluateLinkParams evaluates every runtime-expression string in a Link
// parameter object. Whole expressions preserve JSON types; mixed templates
// produce strings. Constants remain unchanged.
func EvaluateLinkParams(
	link openrpc.Link,
	context Context,
	policy Policy,
) (jsonvalue.Value, bool, error) {
	if !validPolicy(policy) {
		return jsonvalue.Value{}, false, ErrExpressionPolicy
	}
	params, present := link.Params()
	if !present {
		return jsonvalue.Value{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(params.Bytes()))
	decoder.UseNumber()
	var input any
	// Params is an already validated jsonvalue.Value.
	_ = decoder.Decode(&input)
	if _, ok := input.(map[string]any); !ok {
		return jsonvalue.Value{}, true, ErrUnsupportedValue
	}
	nodes := 0
	output, err := evaluateLinkValue(input, context, policy, &nodes)
	if err != nil {
		return jsonvalue.Value{}, true, err
	}
	// evaluateLinkValue only produces values from a decoded JSON tree.
	encoded, _ := json.Marshal(output)
	if len(encoded) > policy.MaxOutputBytes {
		return jsonvalue.Value{}, true, ErrExpressionLimit
	}
	valuePolicy := jsonvalue.DefaultPolicy()
	valuePolicy.MaxBytes = policy.MaxOutputBytes
	value, err := jsonvalue.Parse(encoded, valuePolicy)
	return value, true, err
}

func evaluateLinkValue(value any, context Context, policy Policy, nodes *int) (any, error) {
	*nodes++
	if *nodes > policy.MaxNodes {
		return nil, ErrExpressionLimit
	}
	switch typed := value.(type) {
	case string:
		if !bytes.Contains([]byte(typed), []byte("${")) {
			return typed, nil
		}
		template, err := Parse(typed, policy)
		if err != nil {
			return nil, err
		}
		result, err := template.Evaluate(context)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(result.Bytes()))
		decoder.UseNumber()
		var decoded any
		// Evaluate always returns a validated jsonvalue.Value.
		_ = decoder.Decode(&decoded)
		return decoded, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			resolved, err := evaluateLinkValue(child, context, policy, nodes)
			if err != nil {
				return nil, err
			}
			result[index] = resolved
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := typed[name]
			resolved, err := evaluateLinkValue(child, context, policy, nodes)
			if err != nil {
				return nil, err
			}
			result[name] = resolved
		}
		return result, nil
	default:
		return value, nil
	}
}
