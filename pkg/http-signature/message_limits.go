package httpsignature

import (
	"math"
	"net/http"
	"strconv"

	"github.com/dunglas/httpsfv"
)

func structuredItemFits(name string, parameters []Parameter, budget int) bool {
	return structuredItemUpperBound(name, parameters) <= budget
}

func structuredItemUpperBound(name string, parameters []Parameter) int {
	// Valid component names contain no quoted-string escape characters, and an
	// invalid name is rejected before marshaling.
	size := saturatingSizeAdd(2, len(name))
	for _, parameter := range parameters {
		size = saturatingSizeAdd(size, parameterUpperBound(parameter))
	}
	return size
}

func signatureParametersFit(input SignatureInput, budget int) bool {
	remaining := budget
	if !consumeSize(&remaining, 2) { // opening and closing inner-list parentheses
		return false
	}
	for index, component := range input.Components {
		componentSize := structuredItemUpperBound(component.Name, component.Parameters)
		if index != 0 {
			componentSize = saturatingSizeAdd(componentSize, 1)
		}
		if !consumeSize(&remaining, componentSize) {
			return false
		}
	}
	for _, parameter := range input.Parameters {
		if !consumeSize(&remaining, parameterUpperBound(parameter)) {
			return false
		}
	}
	return true
}

func parameterUpperBound(parameter Parameter) int {
	size := saturatingSizeAdd(1, len(parameter.Name)) // semicolon and name
	valueBytes := 0
	switch value := parameter.Value.(type) {
	case bool:
		if !value {
			valueBytes = 3 // =?0
		}
	case string:
		valueBytes = saturatingSizeAdd(3, escapedStringLength(value)) // = plus quotes and escaping
	case int64:
		valueBytes = 1 + decimalIntegerLength(value)
	case float64:
		serialized, serializeErr := marshalRFC8941(httpsfv.NewItem(value))
		if serializeErr == nil {
			valueBytes = saturatingSizeAdd(1, len(serialized))
		} else {
			valueBytes = 1
		}
	case []byte:
		valueBytes = saturatingSizeAdd(3, base64EncodedLength(len(value))) // '=' and two colons
	case SFToken:
		valueBytes = saturatingSizeAdd(1, len(value))
	default:
		// Invalid types are rejected by semantic serialization without copying a
		// caller-controlled payload.
		valueBytes = 1
	}
	return saturatingSizeAdd(size, valueBytes)
}

func fieldValuesFit(values []string, binary bool, budget int) bool {
	if budget < 0 {
		return false
	}
	remaining := budget
	for index, value := range values {
		if index != 0 && !consumeSize(&remaining, 2) {
			return false
		}
		valueSize := len(value)
		if binary {
			valueSize = saturatingSizeAdd(base64EncodedLength(valueSize), 2)
		}
		if !consumeSize(&remaining, valueSize) {
			return false
		}
	}
	return true
}

func derivedSourceFits(request *http.Request, external *ExternalRequestContext, name string, budget int) bool {
	if request == nil || request.URL == nil || budget < 0 {
		return true
	}
	scheme := request.URL.Scheme
	authority := request.Host
	if authority == "" {
		authority = request.URL.Host
	}
	targetBytes := len(request.RequestURI)
	if targetBytes == 0 {
		pathBytes := saturatingSizeMultiply(len(request.URL.Path), 3)
		targetBytes = saturatingSizeAdd(pathBytes, len(request.URL.RawPath))
		queryBytes := saturatingSizeMultiply(len(request.URL.RawQuery), 3)
		targetBytes = saturatingSizeAdd(targetBytes, queryBytes)
		targetBytes = saturatingSizeAdd(targetBytes, 2)
		if request.URL.Opaque != "" {
			opaqueBytes := saturatingSizeAdd(len(request.URL.Opaque), len(request.URL.Scheme))
			opaqueBytes = saturatingSizeAdd(opaqueBytes, 2)
			opaqueBytes = saturatingSizeAdd(opaqueBytes, queryBytes)
			targetBytes = max(targetBytes, opaqueBytes)
		}
	}
	if external != nil {
		scheme = external.Scheme
		authority = external.Authority
		targetBytes = len(external.RequestTarget)
	}
	if scheme == "" {
		scheme = "https"
		if request.TLS == nil {
			scheme = "http"
		}
	}

	// requestParts validates and parses every one of these sources regardless of
	// the selected derived component, so none may exceed the same allocation
	// boundary.
	if len(scheme) > budget || len(authority) > budget || targetBytes > budget {
		return false
	}
	switch name {
	case "@scheme":
		return len(scheme) <= budget
	case "@authority":
		return len(authority) <= budget
	case "@query-param":
		return targetBytes <= budget/3
	case "@request-target", "@path", "@query":
		return targetBytes <= budget
	case "@target-uri":
		size := saturatingSizeAdd(len(scheme), 3)
		size = saturatingSizeAdd(size, len(authority))
		size = saturatingSizeAdd(size, targetBytes)
		return size <= budget
	default:
		return true
	}
}

func escapedStringLength(value string) int {
	size := len(value)
	for index := range len(value) {
		if value[index] == '"' || value[index] == '\\' {
			size = saturatingSizeAdd(size, 1)
		}
	}
	return size
}

func decimalIntegerLength(value int64) int {
	var buffer [20]byte
	return len(strconv.AppendInt(buffer[:0], value, 10))
}

func safeSizeAdd(left, right int) (int, bool) {
	if left < 0 || right < 0 || left > math.MaxInt-right {
		return 0, false
	}
	return left + right, true
}

func safeSizeMultiply(value, multiplier int) (int, bool) {
	if value < 0 || multiplier < 0 || multiplier != 0 && value > math.MaxInt/multiplier {
		return 0, false
	}
	return value * multiplier, true
}

func saturatingSizeAdd(left, right int) int {
	value, ok := safeSizeAdd(left, right)
	if !ok {
		return math.MaxInt
	}
	return value
}

func saturatingSizeMultiply(value, multiplier int) int {
	result, ok := safeSizeMultiply(value, multiplier)
	if !ok {
		return math.MaxInt
	}
	return result
}

func base64EncodedLength(size int) int {
	groups := saturatingSizeAdd(size, 2) / 3
	return saturatingSizeMultiply(groups, 4)
}

func consumeSize(remaining *int, size int) bool {
	if size < 0 || size > *remaining {
		return false
	}
	*remaining -= size
	return true
}
