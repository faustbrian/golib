package httpsignature

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dunglas/httpsfv"
)

// ErrSignatureBase reports that a covered component cannot be resolved safely.
var ErrSignatureBase = errors.New("http signature: cannot create signature base")

var obsoleteLineFolding = regexp.MustCompile(`\r\n[ \t]+`)

// StructuredFieldType describes the application-known RFC 8941 field shape
// required by the sf component parameter.
type StructuredFieldType uint8

const (
	// StructuredFieldDictionary identifies an RFC 8941 Dictionary field.
	StructuredFieldDictionary StructuredFieldType = iota + 1
	// StructuredFieldList identifies an RFC 8941 List field.
	StructuredFieldList
	// StructuredFieldItem identifies an RFC 8941 Item field.
	StructuredFieldItem
)

// ExternalRequestContext is trusted request-target information supplied by an
// application that terminates HTTP behind an intermediary. Values are never
// inferred from Forwarded or X-Forwarded-* fields.
type ExternalRequestContext struct {
	Scheme        string
	Authority     string
	RequestTarget string
}

// MessageContext supplies the target message and optional related request.
// Exactly one of Request and Response must be set. RelatedRequest is required
// when a response signature covers a component carrying the req parameter.
type MessageContext struct {
	Request          *http.Request
	Response         *http.Response
	RelatedRequest   *http.Request
	ExternalRequest  *ExternalRequestContext
	StructuredFields map[string]StructuredFieldType
}

// CreateSignatureBase resolves the ordered covered components and produces the
// exact RFC 9421 signature base. It returns no partial base on error.
func CreateSignatureBase(context MessageContext, input SignatureInput) (string, error) {
	if (context.Request == nil) == (context.Response == nil) {
		return "", fmt.Errorf("%w: exactly one target message is required", ErrSignatureBase)
	}
	var builder strings.Builder
	seen := make(map[string]struct{}, len(input.Components))
	for _, component := range input.Components {
		identifier, err := serializeComponentIdentifier(component)
		if err != nil {
			return "", fmt.Errorf("%w: invalid component identifier", ErrSignatureBase)
		}
		comparisonKey, _ := componentComparisonKey(component)
		if _, duplicate := seen[comparisonKey]; duplicate {
			return "", fmt.Errorf("%w: duplicate component identifier", ErrSignatureBase)
		}
		seen[comparisonKey] = struct{}{}

		value, err := resolveComponent(context, component)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrSignatureBase, err)
		}
		if strings.HasPrefix(component.Name, "@") && !validDerivedValue(value) ||
			!strings.HasPrefix(component.Name, "@") && !validFieldBaseValue(value) {
			return "", fmt.Errorf("%w: component contains prohibited bytes", ErrSignatureBase)
		}

		builder.WriteString(identifier)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteByte('\n')
	}

	parameters, err := serializeSignatureParameters(input)
	if err != nil {
		return "", fmt.Errorf("%w: invalid signature parameters", ErrSignatureBase)
	}
	builder.WriteString(`"@signature-params": `)
	builder.WriteString(parameters)

	return builder.String(), nil
}

func resolveComponent(context MessageContext, component ComponentIdentifier) (string, error) {
	if component.Name == "@signature-params" {
		return "", errors.New("@signature-params cannot be covered explicitly")
	}

	parameters, err := componentParameterSet(component.Parameters)
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(component.Name, "@") {
		return resolveDerived(context, component.Name, parameters)
	}

	return resolveField(context, component.Name, parameters)
}

type componentParameters struct {
	bs      bool
	sf      bool
	tr      bool
	req     bool
	key     string
	name    string
	hasKey  bool
	hasName bool
}

func componentParameterSet(parameters []Parameter) (componentParameters, error) {
	var result componentParameters
	for _, parameter := range parameters {
		switch parameter.Name {
		case "bs", "sf", "tr", "req":
			value, ok := parameter.Value.(bool)
			if !ok || !value {
				return componentParameters{}, errors.New("flag parameter is not true")
			}
			switch parameter.Name {
			case "bs":
				result.bs = true
			case "sf":
				result.sf = true
			case "tr":
				result.tr = true
			case "req":
				result.req = true
			}
		case "key":
			value, ok := parameter.Value.(string)
			if !ok {
				return componentParameters{}, errors.New("key parameter is not a string")
			}
			result.key, result.hasKey = value, true
		case "name":
			value, ok := parameter.Value.(string)
			if !ok {
				return componentParameters{}, errors.New("name parameter is not a string")
			}
			result.name, result.hasName = value, true
		default:
			return componentParameters{}, errors.New("unknown component parameter")
		}
	}

	if result.bs && (result.sf || result.hasKey) {
		return componentParameters{}, errors.New("bs is incompatible with sf and key")
	}

	return result, nil
}

func resolveDerived(context MessageContext, name string, parameters componentParameters) (string, error) {
	if parameters.bs || parameters.sf || parameters.tr || parameters.hasKey {
		return "", errors.New("field parameter used on derived component")
	}
	if name != "@query-param" && parameters.hasName {
		return "", errors.New("name parameter used outside @query-param")
	}

	request, err := requestForComponent(context, parameters.req)
	if err != nil && name != "@status" {
		return "", err
	}

	switch name {
	case "@method":
		return request.Method, nil
	case "@target-uri", "@authority", "@scheme", "@request-target", "@path", "@query", "@query-param":
		parts, partsErr := requestParts(request, externalForRequest(context, request))
		if partsErr != nil {
			return "", partsErr
		}
		values := map[string]string{
			"@target-uri":     parts.scheme + "://" + parts.authority + parts.originTarget,
			"@authority":      parts.authority,
			"@scheme":         parts.scheme,
			"@request-target": parts.requestTarget,
			"@path":           parts.path,
			"@query":          "?" + parts.rawQuery,
		}
		if name != "@query-param" {
			return values[name], nil
		}
		if !parameters.hasName {
			return "", errors.New("@query-param requires name")
		}
		return queryParameter(parts.rawQuery, parameters.name)
	case "@status":
		if parameters.req || parameters.hasName {
			return "", errors.New("@status has invalid parameters")
		}
		if context.Response == nil || context.Response.StatusCode < 100 || context.Response.StatusCode > 999 {
			return "", errors.New("@status requires a valid response")
		}
		return strconv.Itoa(context.Response.StatusCode), nil
	default:
		return "", errors.New("unknown derived component")
	}

}

func resolveField(context MessageContext, name string, parameters componentParameters) (string, error) {
	if parameters.hasName {
		return "", errors.New("name parameter used on field")
	}

	header, err := headerForComponent(context, parameters.req, parameters.tr)
	if err != nil {
		return "", err
	}
	values := header.Values(name)
	if len(values) == 0 && strings.EqualFold(name, "host") && !parameters.tr {
		request, requestErr := requestForComponent(context, parameters.req)
		if requestErr == nil {
			if request.Host != "" {
				values = []string{request.Host}
			}
		}
	}
	if len(values) == 0 {
		return "", errors.New("covered field is absent")
	}

	if parameters.bs {
		wrapped := make([]string, len(values))
		for index, value := range values {
			normalizedValue, normalizeErr := normalizeFieldBytes(value)
			if normalizeErr != nil {
				return "", normalizeErr
			}
			wrapped[index] = ":" + base64.StdEncoding.EncodeToString([]byte(normalizedValue)) + ":"
		}
		return strings.Join(wrapped, ", "), nil
	}

	normalized := make([]string, len(values))
	for index, value := range values {
		normalizedValue, normalizeErr := normalizeFieldValue(value)
		if normalizeErr != nil {
			return "", normalizeErr
		}
		normalized[index] = normalizedValue
	}

	if parameters.hasKey {
		dictionary, parseErr := unmarshalStructuredDictionary(normalized)
		if parseErr != nil {
			return "", errors.New("covered dictionary field is malformed")
		}
		member, found := dictionary.Get(parameters.key)
		if !found {
			return "", errors.New("covered dictionary member is absent")
		}
		return marshalMember(member), nil
	}

	if parameters.sf {
		fieldType, known := context.StructuredFields[strings.ToLower(name)]
		if !known {
			return "", errors.New("structured field type is unknown")
		}
		return strictStructuredField(normalized, fieldType)
	}

	return strings.Join(normalized, ", "), nil
}

func requestForComponent(context MessageContext, related bool) (*http.Request, error) {
	if related {
		if context.Response == nil || context.RelatedRequest == nil {
			return nil, errors.New("req requires an explicit related request")
		}
		return context.RelatedRequest, nil
	}
	if context.Request != nil {
		return context.Request, nil
	}
	return nil, errors.New("request component requires a request or req")
}

func headerForComponent(context MessageContext, related, trailer bool) (http.Header, error) {
	if related {
		request, err := requestForComponent(context, true)
		if err != nil {
			return nil, err
		}
		if trailer {
			return request.Trailer, nil
		}
		return request.Header, nil
	}
	if context.Request != nil {
		if trailer {
			return context.Request.Trailer, nil
		}
		return context.Request.Header, nil
	}
	if trailer {
		return context.Response.Trailer, nil
	}
	return context.Response.Header, nil
}

type normalizedRequestParts struct {
	scheme        string
	authority     string
	requestTarget string
	originTarget  string
	path          string
	rawQuery      string
}

func requestParts(request *http.Request, external *ExternalRequestContext) (normalizedRequestParts, error) {
	if request == nil || request.URL == nil {
		return normalizedRequestParts{}, errors.New("request URL is unavailable")
	}

	scheme := request.URL.Scheme
	authority := request.URL.Host
	requestTarget := request.RequestURI
	if requestTarget == "" {
		requestTarget = request.URL.RequestURI()
	}
	if external != nil {
		if external.Scheme == "" {
			return normalizedRequestParts{}, errors.New("external request context must be complete")
		}
		if external.Authority == "" {
			return normalizedRequestParts{}, errors.New("external request context must be complete")
		}
		if external.RequestTarget == "" {
			return normalizedRequestParts{}, errors.New("external request context must be complete")
		}
		scheme = external.Scheme
		authority = external.Authority
		requestTarget = external.RequestTarget
	}

	var targetURL *url.URL
	emptyTargetPathQuery := false
	lowerTarget := strings.ToLower(requestTarget)
	absoluteHTTP := strings.HasPrefix(lowerTarget, "http://") || strings.HasPrefix(lowerTarget, "https://")
	switch {
	case absoluteHTTP:
		parsedTarget, targetErr := url.Parse(requestTarget)
		if targetErr != nil {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if parsedTarget.Host == "" {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if parsedTarget.User != nil || parsedTarget.Fragment != "" || parsedTarget.Opaque != "" {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		targetURL = parsedTarget
		if external == nil {
			scheme = parsedTarget.Scheme
			authority = parsedTarget.Host
		} else {
			targetScheme := strings.ToLower(parsedTarget.Scheme)
			externalScheme := strings.ToLower(external.Scheme)
			targetAuthority, targetAuthorityErr := normalizeAuthority(parsedTarget.Host, targetScheme)
			externalAuthority, externalAuthorityErr := normalizeAuthority(external.Authority, externalScheme)
			if targetScheme != externalScheme {
				return normalizedRequestParts{}, errors.New("external request context contradicts absolute target")
			}
			if targetAuthorityErr != nil {
				return normalizedRequestParts{}, errors.New("external request context contradicts absolute target")
			}
			if externalAuthorityErr != nil {
				return normalizedRequestParts{}, errors.New("external request context contradicts absolute target")
			}
			if targetAuthority != externalAuthority {
				return normalizedRequestParts{}, errors.New("external request context contradicts absolute target")
			}
		}
	case strings.HasPrefix(requestTarget, "/"):
		parsedTarget, targetErr := url.ParseRequestURI(requestTarget)
		if targetErr != nil {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if strings.Contains(requestTarget, "#") {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		targetURL = parsedTarget
	case requestTarget == "*":
		if request.Method != http.MethodOptions {
			return normalizedRequestParts{}, errors.New("asterisk request target requires OPTIONS")
		}
		targetURL = &url.URL{}
		emptyTargetPathQuery = true
	case request.Method == http.MethodConnect:
		parsedAuthority, targetErr := url.Parse("//" + requestTarget)
		if targetErr != nil {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if parsedAuthority.Host == "" {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if parsedAuthority.Port() == "" || parsedAuthority.Path != "" || parsedAuthority.User != nil ||
			parsedAuthority.RawQuery != "" || parsedAuthority.Fragment != "" {
			return normalizedRequestParts{}, errors.New("request target is malformed")
		}
		if external == nil {
			authority = parsedAuthority.Host
		} else {
			externalScheme := strings.ToLower(external.Scheme)
			targetAuthority, targetAuthorityErr := normalizeAuthority(parsedAuthority.Host, externalScheme)
			externalAuthority, externalAuthorityErr := normalizeAuthority(external.Authority, externalScheme)
			if targetAuthorityErr != nil {
				return normalizedRequestParts{}, errors.New("external request context contradicts authority target")
			}
			if externalAuthorityErr != nil {
				return normalizedRequestParts{}, errors.New("external request context contradicts authority target")
			}
			if targetAuthority != externalAuthority {
				return normalizedRequestParts{}, errors.New("external request context contradicts authority target")
			}
		}
		targetURL = &url.URL{}
		emptyTargetPathQuery = true
	default:
		return normalizedRequestParts{}, errors.New("request target is malformed")
	}

	if scheme == "" {
		if request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return normalizedRequestParts{}, errors.New("request scheme is not HTTP")
	}
	if authority == "" {
		authority = request.Host
	}
	normalizedAuthority, err := normalizeAuthority(authority, scheme)
	if err != nil {
		return normalizedRequestParts{}, err
	}
	rawPath := targetURL.EscapedPath()
	path := rawPath
	if path == "" {
		path = "/"
	}
	originTarget := rawPath
	rawQuery := targetURL.RawQuery
	if emptyTargetPathQuery {
		rawQuery = ""
	} else if targetURL.ForceQuery || rawQuery != "" {
		originTarget += "?" + rawQuery
	}

	return normalizedRequestParts{
		scheme:        scheme,
		authority:     normalizedAuthority,
		requestTarget: requestTarget,
		originTarget:  originTarget,
		path:          path,
		rawQuery:      rawQuery,
	}, nil
}

func externalForRequest(context MessageContext, request *http.Request) *ExternalRequestContext {
	if request == context.Request {
		return context.ExternalRequest
	}
	if request == context.RelatedRequest {
		return context.ExternalRequest
	}
	return nil
}

func normalizeAuthority(authority, scheme string) (string, error) {
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("invalid request authority")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !asciiString(host) {
		return "", errors.New("invalid request host")
	}
	port := parsed.Port()
	if port != "" {
		numericPort, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil {
			return "", errors.New("invalid request port")
		}
		port = strconv.FormatUint(numericPort, 10)
		if port == "80" && scheme == "http" || port == "443" && scheme == "https" {
			port = ""
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		return net.JoinHostPort(strings.Trim(host, "[]"), port), nil
	}

	return host, nil
}

func normalizeFieldValue(value string) (string, error) {
	value, err := normalizeFieldBytes(value)
	if err != nil {
		return "", err
	}
	if !asciiString(value) {
		return "", errors.New("field value contains prohibited bytes")
	}
	return value, nil
}

func normalizeFieldBytes(value string) (string, error) {
	value = strings.Trim(value, " \t")
	value = obsoleteLineFolding.ReplaceAllString(value, " ")
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("field value contains prohibited bytes")
	}
	return value, nil
}

func strictStructuredField(values []string, fieldType StructuredFieldType) (string, error) {
	var value httpsfv.StructuredFieldValue
	var err error
	switch fieldType {
	case StructuredFieldDictionary:
		value, err = unmarshalStructuredDictionary(normalizeStructuredFieldOWS(values))
	case StructuredFieldList:
		value, err = unmarshalStructuredList(normalizeStructuredFieldOWS(values))
	case StructuredFieldItem:
		if len(values) != 1 {
			return "", errors.New("item field has multiple values")
		}
		item, itemErr := unmarshalStructuredItem(values)
		value, err = item, itemErr
	default:
		return "", errors.New("unknown structured field type")
	}
	if err != nil {
		return "", errors.New("malformed structured field")
	}
	serialized, _ := httpsfv.Marshal(value)
	return serialized, nil
}

func marshalMember(member httpsfv.Member) string {
	dictionary := httpsfv.NewDictionary()
	dictionary.Add("x", member)
	serialized, _ := httpsfv.Marshal(dictionary)
	separator := strings.IndexByte(serialized, '=')
	if separator == -1 {
		return "?1" + strings.TrimPrefix(serialized, "x")
	}
	return serialized[separator+1:]
}

func serializeComponentIdentifier(component ComponentIdentifier) (string, error) {
	if !validComponentName(component.Name) || !validParametersForSerialization(component.Parameters) {
		return "", errors.New("invalid component identifier")
	}
	item := httpsfv.NewItem(component.Name)
	addParameters(item.Params, component.Parameters)
	return httpsfv.Marshal(item)
}

func serializeSignatureParameters(input SignatureInput) (string, error) {
	if !validParametersForSerialization(input.Parameters) {
		return "", errors.New("invalid signature parameters")
	}
	items := make([]httpsfv.Item, len(input.Components))
	for index, component := range input.Components {
		if !validComponentName(component.Name) || !validParametersForSerialization(component.Parameters) {
			return "", errors.New("invalid component identifier")
		}
		items[index] = httpsfv.NewItem(component.Name)
		addParameters(items[index].Params, component.Parameters)
	}
	parameters := httpsfv.NewParams()
	addParameters(parameters, input.Parameters)
	return httpsfv.Marshal(httpsfv.InnerList{Items: items, Params: parameters})
}

func queryParameter(rawQuery, encodedName string) (string, error) {
	if rawQuery == "" {
		return "", errors.New("named query parameter is absent")
	}
	var found string
	foundCount := 0
	for _, pair := range strings.Split(rawQuery, "&") {
		name, value, hasValue := strings.Cut(pair, "=")
		if !hasValue {
			value = ""
		}
		decodedName, err := url.QueryUnescape(name)
		if err != nil {
			return "", errors.New("malformed query parameter name")
		}
		decodedName = strings.ToValidUTF8(decodedName, "\uFFFD")
		if formPercentEncode(decodedName) != encodedName {
			continue
		}
		decodedValue, err := url.QueryUnescape(value)
		if err != nil {
			return "", errors.New("malformed query parameter value")
		}
		decodedValue = strings.ToValidUTF8(decodedValue, "\uFFFD")
		found = formPercentEncode(decodedValue)
		foundCount++
	}
	if foundCount != 1 {
		return "", errors.New("named query parameter is absent or repeated")
	}
	return found, nil
}

func formPercentEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '*' || character == '-' ||
			character == '.' || character == '_' {
			builder.WriteByte(character)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hex[character>>4])
		builder.WriteByte(hex[character&0x0f])
	}
	return builder.String()
}

func validDerivedValue(value string) bool {
	if value == "" || value[0] == ' ' || value[len(value)-1] == ' ' || value[0] == '\t' || value[len(value)-1] == '\t' {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func validFieldBaseValue(value string) bool {
	for index := range len(value) {
		character := value[index]
		if character != '\t' && (character < 0x20 || character > 0x7e) {
			return false
		}
	}
	return true
}

func validParametersForSerialization(parameters []Parameter) bool {
	for _, parameter := range parameters {
		if parameter.Name == "" || !isKeyStart(parameter.Name[0]) {
			return false
		}
		for index := 1; index < len(parameter.Name); index++ {
			if !isKeyCharacter(parameter.Name[index]) {
				return false
			}
		}
		switch parameter.Value.(type) {
		case bool, string, int64, float64, []byte, SFToken:
		default:
			return false
		}
	}

	return true
}

func asciiString(value string) bool {
	for index := range len(value) {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}
