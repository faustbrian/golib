package router

import (
	"net"
	"net/url"
	"strings"
	"unicode"
)

type parameterKind uint8

const (
	segmentParameter parameterKind = iota
	remainderParameter
	maxTrustedAuthorityBytes = 261
	maxRemainderSegments     = 65_536
)

// URLParameter is an explicitly typed named-route generation input. Construct
// values with Param or Remainder.
type URLParameter struct {
	name      string
	values    []string
	kind      parameterKind
	oversized bool
}

// Param supplies one path segment or host label.
func Param(name, value string) URLParameter {
	return URLParameter{name: name, values: []string{value}, kind: segmentParameter}
}

// Remainder supplies explicit path segments for a remainder wildcard. Inputs
// above the package hard ceiling are rejected during generation without being
// copied.
func Remainder(name string, segments ...string) URLParameter {
	if len(segments) > maxRemainderSegments {
		return URLParameter{name: name, kind: remainderParameter, oversized: true}
	}
	return URLParameter{name: name, values: append([]string(nil), segments...), kind: remainderParameter}
}

// BaseURL is an immutable validated absolute-URL base.
type BaseURL struct {
	scheme string
	host   string
}

// NewBaseURL validates and trusts one explicit HTTP or HTTPS authority.
func NewBaseURL(scheme, authority string) (BaseURL, error) {
	if len(scheme) > len("https") {
		return BaseURL{}, generationError(ErrLimitExceeded, "scheme", "scheme is too long")
	}
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return BaseURL{}, generationError(ErrGeneration, "scheme", "scheme must be http or https")
	}
	if len(authority) > maxTrustedAuthorityBytes {
		return BaseURL{}, generationError(ErrLimitExceeded, "host", "trusted authority is too long")
	}
	if authority == "" || invalidAuthority(authority) || !ascii(authority) {
		return BaseURL{}, generationError(ErrGeneration, "host", "invalid trusted authority")
	}
	return BaseURL{scheme: scheme, host: authority}, nil
}

type generationRoute struct {
	host string
	path string
}

// Path generates a relative escaped path for a named route. Host wildcard
// values are intentionally not accepted by relative generation.
func (r *Router) Path(name string, parameters ...URLParameter) (string, error) {
	if r == nil {
		return "", generationError(ErrCompileState, "router", "nil compiled router")
	}
	if len(name) > r.limits.MaxNameBytes {
		return "", generationError(ErrLimitExceeded, "name", "route name is too long")
	}
	route, exists := r.named[name]
	if !exists {
		return "", generationError(ErrGeneration, "name", "unknown route name")
	}
	provided, err := collectParameters(parameters, r.limits)
	if err != nil {
		return "", err
	}
	path, used, err := renderPath(route.path, provided)
	if err != nil {
		return "", err
	}
	if err := rejectUnused(provided, used); err != nil {
		return "", err
	}
	if len(path) > r.limits.MaxGeneratedURLBytes {
		return "", generationError(ErrLimitExceeded, "url", "generated path is too long")
	}
	return path, nil
}

// URL generates an absolute URL using a validated explicit base. A route host
// replaces the base hostname while retaining its trusted explicit port.
func (r *Router) URL(name string, base BaseURL, query url.Values, parameters ...URLParameter) (string, error) {
	if r == nil {
		return "", generationError(ErrCompileState, "router", "nil compiled router")
	}
	if base.scheme == "" || base.host == "" {
		return "", generationError(ErrGeneration, "base", "uninitialized base URL")
	}
	if len(name) > r.limits.MaxNameBytes {
		return "", generationError(ErrLimitExceeded, "name", "route name is too long")
	}
	route, exists := r.named[name]
	if !exists {
		return "", generationError(ErrGeneration, "name", "unknown route name")
	}
	provided, err := collectParameters(parameters, r.limits)
	if err != nil {
		return "", err
	}
	path, used, err := renderPath(route.path, provided)
	if err != nil {
		return "", err
	}
	host, hostUsed, err := renderHost(route.host, base.host, provided)
	if err != nil {
		return "", err
	}
	for name := range hostUsed {
		used[name] = struct{}{}
	}
	if err := rejectUnused(provided, used); err != nil {
		return "", err
	}
	if err := validateQuery(query, r.limits); err != nil {
		return "", err
	}
	generated := base.scheme + "://" + host + path
	if encoded := query.Encode(); encoded != "" {
		generated += "?" + encoded
	}
	if len(generated) > r.limits.MaxGeneratedURLBytes {
		return "", generationError(ErrLimitExceeded, "url", "generated URL is too long")
	}
	return generated, nil
}

func collectParameters(parameters []URLParameter, limits Limits) (map[string]URLParameter, error) {
	if len(parameters) > limits.MaxURLParameters {
		return nil, generationError(ErrLimitExceeded, "parameters", "parameter count exceeded")
	}
	provided := make(map[string]URLParameter, len(parameters))
	bytes := 0
	values := 0
	for _, parameter := range parameters {
		if parameter.oversized {
			return nil, generationError(ErrLimitExceeded, "parameters", "remainder segment count exceeded")
		}
		if len(parameter.name) > limits.MaxWildcardNameBytes || len(parameter.name) > limits.MaxURLParameterBytes-bytes {
			return nil, generationError(ErrLimitExceeded, "parameters", "parameter bytes exceeded")
		}
		bytes += len(parameter.name)
		if len(parameter.values) > limits.MaxURLParameters-values {
			return nil, generationError(ErrLimitExceeded, "parameters", "parameter value count exceeded")
		}
		values += len(parameter.values)
		for _, value := range parameter.values {
			if len(value) > limits.MaxURLParameterBytes-bytes {
				return nil, generationError(ErrLimitExceeded, "parameters", "parameter bytes exceeded")
			}
			bytes += len(value)
		}
		if !validWildcardName(parameter.name) {
			return nil, generationError(ErrInvalidParameter, "parameters", "invalid parameter name")
		}
		if _, duplicate := provided[parameter.name]; duplicate {
			return nil, generationError(ErrInvalidParameter, "parameters", "duplicate parameter")
		}
		provided[parameter.name] = parameter
	}
	return provided, nil
}

func validWildcardName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if unicode.IsLetter(character) || character == '_' ||
			index > 0 && unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return true
}

func renderPath(pattern string, provided map[string]URLParameter) (string, map[string]struct{}, error) {
	segments := strings.Split(pattern, "/")
	output := make([]string, 0, len(segments))
	used := make(map[string]struct{})
	for _, segment := range segments {
		if segment == "{$}" {
			output = append(output, "")
			continue
		}
		name, kind, wildcard := pathWildcard(segment)
		if !wildcard {
			output = append(output, segment)
			continue
		}
		parameter, exists := provided[name]
		if !exists {
			return "", nil, generationError(ErrInvalidParameter, "parameters", "missing path parameter")
		}
		if parameter.kind != kind || len(parameter.values) == 0 || kind == segmentParameter && len(parameter.values) != 1 {
			return "", nil, generationError(ErrInvalidParameter, "parameters", "wrong parameter kind")
		}
		for _, value := range parameter.values {
			if !safePathSegment(value) {
				return "", nil, generationError(ErrInvalidParameter, "parameters", "unsafe path segment")
			}
			output = append(output, url.PathEscape(value))
		}
		used[name] = struct{}{}
	}
	return strings.Join(output, "/"), used, nil
}

func renderHost(pattern, baseAuthority string, provided map[string]URLParameter) (string, map[string]struct{}, error) {
	if pattern == "" {
		return baseAuthority, map[string]struct{}{}, nil
	}
	labels := strings.Split(pattern, ".")
	used := make(map[string]struct{})
	for index, label := range labels {
		if !isWildcardLabel(label) {
			continue
		}
		name := label[1 : len(label)-1]
		parameter, exists := provided[name]
		if !exists {
			return "", nil, generationError(ErrInvalidParameter, "parameters", "missing host parameter")
		}
		if parameter.kind != segmentParameter || len(parameter.values) != 1 || !safeHostLabel(parameter.values[0]) {
			return "", nil, generationError(ErrInvalidParameter, "parameters", "unsafe host label")
		}
		labels[index] = parameter.values[0]
		used[name] = struct{}{}
	}
	host := strings.Join(labels, ".")
	parsed, _ := url.Parse("//" + baseAuthority)
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return host, used, nil
}

func pathWildcard(segment string) (string, parameterKind, bool) {
	if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
		return "", segmentParameter, false
	}
	name := segment[1 : len(segment)-1]
	if strings.HasSuffix(name, "...") {
		return strings.TrimSuffix(name, "..."), remainderParameter, true
	}
	return name, segmentParameter, true
}

func rejectUnused(provided map[string]URLParameter, used map[string]struct{}) error {
	for name := range provided {
		if _, exists := used[name]; !exists {
			return generationError(ErrInvalidParameter, "parameters", "unknown or unused parameter")
		}
	}
	return nil
}

func safePathSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		strings.Trim(value, "/") != "" && !strings.ContainsAny(value, "\x00\r\n")
}

func safeHostLabel(value string) bool {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validateQuery(values url.Values, limits Limits) error {
	if len(values) > limits.MaxQueryValues {
		return generationError(ErrLimitExceeded, "query", "query value count exceeded")
	}
	count := 0
	bytes := 0
	for key, entries := range values {
		if len(key) > limits.MaxQueryBytes-bytes {
			return generationError(ErrLimitExceeded, "query", "query bytes exceeded")
		}
		bytes += len(key)
		if len(entries) == 0 {
			count++
		} else {
			count += len(entries)
			for _, entry := range entries {
				if len(entry) > limits.MaxQueryBytes-bytes {
					return generationError(ErrLimitExceeded, "query", "query bytes exceeded")
				}
				bytes += len(entry)
			}
		}
		if count > limits.MaxQueryValues {
			return generationError(ErrLimitExceeded, "query", "query value count exceeded")
		}
	}
	return nil
}

func generationError(kind error, field, detail string) error {
	return &Error{Kind: kind, Field: field, Detail: detail}
}

func ascii(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}
