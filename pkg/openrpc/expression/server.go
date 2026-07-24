package expression

import (
	"encoding/json"
	"errors"
	"strconv"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrUnknownVariable reports an override absent from server.variables.
	ErrUnknownVariable = errors.New("expression: unknown server variable")
	// ErrVariableEnum reports a default or override outside a declared enum.
	ErrVariableEnum = errors.New("expression: server variable outside enum")
)

// EvaluateServer applies server-variable defaults and caller overrides to one
// OpenRPC Server URL. Overrides and defaults must satisfy declared enums.
func EvaluateServer(server openrpc.Server, overrides map[string]string, policy Policy) (string, error) {
	variables, _ := server.Variables()
	for name := range overrides {
		if _, exists := variables[name]; !exists {
			return "", ErrUnknownVariable
		}
	}
	bindings := make(map[string]jsonvalue.Value, len(variables))
	for name, variable := range variables {
		value := variable.Default()
		if override, exists := overrides[name]; exists {
			value = override
		}
		if enumeration, present := variable.Enum(); present && !contains(enumeration, value) {
			return "", ErrVariableEnum
		}
		encoded, _ := jsonvalue.Parse([]byte(strconv.Quote(value)), jsonvalue.DefaultPolicy())
		bindings[name] = encoded
	}
	context, _ := NewContext(bindings)
	template, err := Parse(server.URL(), policy)
	if err != nil {
		return "", err
	}
	result, err := template.Evaluate(context)
	if err != nil {
		return "", err
	}
	var output string
	// String bindings and literal URL text can only produce a JSON string.
	_ = json.Unmarshal(result.Bytes(), &output)
	return output, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
