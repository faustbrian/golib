package cloudevents

import "strings"

// NewTraceParentAttribute constructs the selected distributed-tracing
// extension's W3C traceparent value.
func NewTraceParentAttribute(value string) (Attribute, error) {
	if !validTraceParent(value) {
		return Attribute{}, ErrInvalidAttribute
	}
	return NewStringAttribute(value)
}

// NewTraceStateAttribute constructs the selected distributed-tracing
// extension's W3C tracestate value.
func NewTraceStateAttribute(value string) (Attribute, error) {
	if !validTraceState(value) {
		return Attribute{}, ErrInvalidAttribute
	}
	return NewStringAttribute(value)
}

// NewPartitionKeyAttribute constructs the selected partitioning extension.
func NewPartitionKeyAttribute(value string) (Attribute, error) {
	if value == "" {
		return Attribute{}, ErrInvalidAttribute
	}
	return NewStringAttribute(value)
}

func validTraceParent(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || !lowerHex(part) {
			return false
		}
	}
	if parts[0] == "ff" || allZero(parts[1]) || allZero(parts[2]) {
		return false
	}
	if parts[0] == "00" && len(parts) != 4 {
		return false
	}
	return true
}

func lowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func allZero(value string) bool {
	for _, character := range value {
		if character != '0' {
			return false
		}
	}
	return true
}

func validTraceState(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	if value[0] == ' ' || value[0] == '\t' || value[len(value)-1] == ' ' || value[len(value)-1] == '\t' {
		return false
	}
	members := strings.Split(value, ",")
	if len(members) > 32 {
		return false
	}
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		member = strings.Trim(member, " \t")
		key, stateValue, present := strings.Cut(member, "=")
		if !present || !validTraceStateKey(key) || !validTraceStateValue(stateValue) {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validTraceStateKey(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	if strings.Count(value, "@") == 0 {
		return len(value) <= 256 && value[0] >= 'a' && value[0] <= 'z' && validTraceStateKeyPart(value)
	}
	tenant, system, _ := strings.Cut(value, "@")
	if len(tenant) == 0 || len(tenant) > 241 || len(system) == 0 || len(system) > 14 {
		return false
	}
	tenantStartsLower := tenant[0] >= 'a' && tenant[0] <= 'z'
	tenantStartsDigit := tenant[0] >= '0' && tenant[0] <= '9'
	systemStartsLower := system[0] >= 'a' && system[0] <= 'z'
	if (!tenantStartsLower && !tenantStartsDigit) || !systemStartsLower {
		return false
	}
	return validTraceStateKeyPart(tenant) && validTraceStateKeyPart(system)
}

func validTraceStateKeyPart(value string) bool {
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '*' && character != '/' {
			return false
		}
	}
	return true
}

func validTraceStateValue(value string) bool {
	if value == "" || len(value) > 256 || value[len(value)-1] == ' ' {
		return false
	}
	// W3C Trace Context narrows list-member values to printable ASCII except the
	// comma and equals delimiters.
	for _, character := range []byte(value) {
		if character < 0x20 || character > 0x7e || character == ',' || character == '=' {
			return false
		}
	}
	return true
}
