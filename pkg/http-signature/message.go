package httpsignature

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dunglas/httpsfv"
)

var (
	// ErrSignatureBase reports that a covered component cannot be resolved safely.
	ErrSignatureBase = errors.New("http signature: cannot create signature base")
	// ErrSignatureBaseLimit reports that canonicalization exceeded its explicit
	// output bound. It wraps ErrSignatureBase so existing classification remains
	// fail closed.
	ErrSignatureBaseLimit = fmt.Errorf("%w: resource limit exceeded", ErrSignatureBase)
)

// DefaultMaxSignatureBaseBytes is the bounded default for direct callers that
// do not select a stricter per-message limit.
const DefaultMaxSignatureBaseBytes = 1 << 20

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

// ResponseTransportMode selects the net/http response representation whose
// transport-managed fields are covered by a signature.
type ResponseTransportMode uint8

const (
	// ResponseTransportUnspecified accepts a managed response field only when
	// the received and Response.Write representations are provably identical.
	ResponseTransportUnspecified ResponseTransportMode = iota
	// ResponseTransportReceived covers a response parsed by net/http or returned
	// by a RoundTripper. Preserved Header values carry received field identity.
	ResponseTransportReceived
	// ResponseTransportWrite covers the deterministic output of Response.Write.
	ResponseTransportWrite
)

// MessageContext supplies the target message and optional related request.
// Exactly one of Request and Response must be set. RelatedRequest is required
// when a response signature covers a component carrying the req parameter.
type MessageContext struct {
	Request           *http.Request
	Response          *http.Response
	RelatedRequest    *http.Request
	ExternalRequest   *ExternalRequestContext
	StructuredFields  map[string]StructuredFieldType
	ResponseTransport ResponseTransportMode
	// MaxSignatureBaseBytes bounds the complete canonical base. Zero selects
	// DefaultMaxSignatureBaseBytes; negative values are invalid.
	MaxSignatureBaseBytes int
}

// CreateSignatureBase resolves the ordered covered components and produces the
// exact RFC 9421 signature base. It returns no partial base on error.
func CreateSignatureBase(context MessageContext, input SignatureInput) (string, error) {
	if (context.Request == nil) == (context.Response == nil) {
		return "", fmt.Errorf("%w: exactly one target message is required", ErrSignatureBase)
	}
	if context.ResponseTransport > ResponseTransportWrite || context.Request != nil && context.ResponseTransport != ResponseTransportUnspecified {
		return "", fmt.Errorf("%w: invalid response transport mode", ErrSignatureBase)
	}
	limit := context.MaxSignatureBaseBytes
	if limit == 0 {
		limit = DefaultMaxSignatureBaseBytes
	}
	if uint(limit) > uint(math.MaxInt) {
		return "", ErrSignatureBaseLimit
	}
	var builder strings.Builder
	remaining := limit
	seen := make(map[string]struct{})
	for _, component := range input.Components {
		if !structuredItemFits(component.Name, component.Parameters, remaining) {
			return "", ErrSignatureBaseLimit
		}
		identifier, err := serializeComponentIdentifier(component)
		if err != nil {
			return "", fmt.Errorf("%w: invalid component identifier", ErrSignatureBase)
		}
		comparisonKey, _ := componentComparisonKey(component)
		if _, duplicate := seen[comparisonKey]; duplicate {
			return "", fmt.Errorf("%w: duplicate component identifier", ErrSignatureBase)
		}
		seen[comparisonKey] = struct{}{}

		fixedBytes := saturatingSizeAdd(len(identifier), len(": ")+len("\n"))
		if !consumeSize(&remaining, fixedBytes) {
			return "", ErrSignatureBaseLimit
		}
		value, err := resolveComponent(context, component, remaining)
		if err != nil {
			if errors.Is(err, ErrSignatureBaseLimit) {
				return "", ErrSignatureBaseLimit
			}
			return "", fmt.Errorf("%w: %v", ErrSignatureBase, err)
		}
		switch component.Name[0] {
		case '@':
			switch component.Name {
			case "@query-param":
			default:
				if !validDerivedValue(value) {
					return "", fmt.Errorf("%w: component contains prohibited bytes", ErrSignatureBase)
				}
			}
		default:
		}
		// Every resolver receives this remaining budget and returns either a value
		// within it or ErrSignatureBaseLimit, so subtraction cannot underflow.
		remaining -= len(value)

		// The identifier framing and resolved value were bounded against the
		// remaining builder capacity above, so these writes cannot cross limit.
		builder.WriteString(identifier)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteByte('\n')
	}

	parameterPrefix := `"@signature-params": `
	if !consumeSize(&remaining, len(parameterPrefix)) || !signatureParametersFit(input, remaining) {
		return "", ErrSignatureBaseLimit
	}
	parameters, err := serializeSignatureParameters(input)
	if err != nil {
		return "", fmt.Errorf("%w: invalid signature parameters", ErrSignatureBase)
	}
	builder.WriteString(parameterPrefix)
	builder.WriteString(parameters)

	return builder.String(), nil
}

func resolveComponent(context MessageContext, component ComponentIdentifier, maxBytes int) (string, error) {
	if component.Name == "@signature-params" {
		return "", errors.New("@signature-params cannot be covered explicitly")
	}

	parameters, err := componentParameterSet(component.Parameters)
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(component.Name, "@") {
		return resolveDerived(context, component.Name, parameters, maxBytes)
	}

	return resolveField(context, component.Name, parameters, maxBytes)
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

func resolveDerived(context MessageContext, name string, parameters componentParameters, maxBytes int) (string, error) {
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
		if len(request.Method) > maxBytes {
			return "", ErrSignatureBaseLimit
		}
		if !validHTTPToken(request.Method) {
			return "", errors.New("request method is invalid")
		}
		return request.Method, nil
	case "@target-uri", "@authority", "@scheme", "@request-target", "@path", "@query", "@query-param":
		if !derivedSourceFits(request, externalForRequest(context, request), name, maxBytes) {
			return "", ErrSignatureBaseLimit
		}
		parts, partsErr := requestParts(request, externalForRequest(context, request))
		if partsErr != nil {
			return "", partsErr
		}
		if name != "@query-param" {
			var value string
			switch name {
			case "@target-uri":
				value = parts.scheme + "://" + parts.authority + parts.originTarget
			case "@authority":
				value = parts.authority
			case "@scheme":
				value = parts.scheme
			case "@request-target":
				value = parts.requestTarget
			case "@path":
				value = parts.path
			case "@query":
				value = "?" + parts.rawQuery
			}
			return value, nil
		}
		if !parameters.hasName {
			return "", errors.New("@query-param requires name")
		}
		return queryParameter(parts.rawQuery, parameters.name, maxBytes)
	case "@status":
		if parameters.req || parameters.hasName {
			return "", errors.New("@status has invalid parameters")
		}
		if context.Response == nil || context.Response.StatusCode < 100 || context.Response.StatusCode > 599 {
			return "", errors.New("@status requires a valid response")
		}
		return strconv.Itoa(context.Response.StatusCode), nil
	default:
		return "", errors.New("unknown derived component")
	}

}

func resolveField(context MessageContext, name string, parameters componentParameters, maxBytes int) (string, error) {
	if parameters.hasName {
		return "", errors.New("name parameter used on field")
	}

	requestOwned := !parameters.tr && (parameters.req || context.Request != nil)
	values, handled, valuesErr := managedFieldValues(context, name, parameters, requestOwned)
	if valuesErr != nil {
		return "", valuesErr
	}
	if !handled {
		header, _ := headerForComponent(context, parameters.req, parameters.tr)
		values, valuesErr = caseInsensitiveHeaderValues(header, name)
		if valuesErr != nil {
			return "", valuesErr
		}
	}
	if len(values) == 0 {
		return "", errors.New("covered field is absent")
	}
	if !parameters.bs && strings.EqualFold(name, "set-cookie") && len(values) != 1 {
		return "", errors.New("multiple Set-Cookie fields require binary wrapping")
	}
	if requestOwned && strings.EqualFold(name, "cookie") && !parameters.bs {
		if len(values) != 1 {
			return "", errors.New("cookie coverage requires one canonical field value")
		}
		normalizedCookie, normalizeErr := normalizeFieldValue(values[0])
		if normalizeErr != nil {
			return "", normalizeErr
		}
		canonicalCookie, validCookie := canonicalCookieValue(normalizedCookie)
		if !validCookie || canonicalCookie != normalizedCookie {
			return "", errors.New("cookie coverage requires canonical semicolon spacing")
		}
	}
	if !fieldValuesFit(values, parameters.bs, maxBytes) {
		return "", ErrSignatureBaseLimit
	}
	if componentHTTPMajor(context, parameters.req) != 1 {
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return "", errors.New("field value contains HTTP/1.1-only line folding")
			}
		}
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

func managedFieldValues(context MessageContext, name string, parameters componentParameters, requestOwned bool) ([]string, bool, error) {
	if requestOwned && strings.EqualFold(name, "host") {
		request, requestErr := requestForComponent(context, parameters.req)
		if requestErr != nil {
			return nil, true, requestErr
		}
		host := request.Host
		if host == "" && request.URL != nil {
			host = request.URL.Host
		}
		host = removeIPv6Zone(host)
		if host == "" {
			return nil, true, nil
		}
		if !validNetHTTPHostHeader(host) {
			return nil, true, errors.New("request Host is not representable on the net/http wire")
		}
		return []string{host}, true, nil
	}
	if requestOwned && strings.EqualFold(name, "content-length") {
		request, requestErr := requestForComponent(context, parameters.req)
		if requestErr != nil {
			return nil, true, requestErr
		}
		if contentLength, ok := requestContentLength(request); ok {
			return []string{strconv.FormatInt(contentLength, 10)}, true, nil
		}
		return nil, true, nil
	}
	if requestOwned {
		request, requestErr := requestForComponent(context, parameters.req)
		if requestErr != nil {
			return nil, true, requestErr
		}
		values, handled, managedErr := requestTransportFieldValues(request, name)
		if handled || managedErr != nil {
			return values, true, managedErr
		}
	}
	if context.Response == nil {
		return nil, false, nil
	}
	if parameters.req {
		return nil, false, nil
	}
	if parameters.tr {
		return nil, false, nil
	}
	if strings.EqualFold(name, "content-length") {
		values, contentLengthErr := responseContentLengthFieldValues(context.Response, context.ResponseTransport)
		return values, true, contentLengthErr
	}
	values, handled, managedErr := responseTransportFieldValues(context.Response, context.ResponseTransport, name)
	if handled || managedErr != nil {
		return values, true, managedErr
	}
	return nil, false, nil
}

func canonicalCookieValue(value string) (string, bool) {
	parts := strings.Split(value, ";")
	for index := range parts {
		parts[index] = strings.Trim(parts[index], " \t")
		if parts[index] == "" {
			return "", false
		}
	}
	return strings.Join(parts, "; "), true
}

func requestTransportFieldValues(request *http.Request, name string) ([]string, bool, error) {
	switch {
	case strings.EqualFold(name, "transfer-encoding"):
		if request.Body == nil {
			return nil, true, nil
		}
		values, err := transferEncodingFieldValues(request.TransferEncoding)
		return values, true, err
	case strings.EqualFold(name, "trailer"):
		if request.RequestURI != "" {
			return nil, true, errors.New("inbound Trailer declaration order is unavailable")
		}
		if !requestUsesChunkedTransferEncoding(request) || len(request.Trailer) == 0 {
			return nil, true, nil
		}
		values, err := trailerDeclarationFieldValues(request.Trailer)
		return values, true, err
	case strings.EqualFold(name, "user-agent") && request.RequestURI == "":
		values, err := caseInsensitiveHeaderValues(request.Header, name)
		if err != nil || len(values) == 0 || values[0] == "" {
			return nil, true, err
		}
		return values[:1], true, nil
	case strings.EqualFold(name, "connection") && request.RequestURI == "" && request.Close:
		values, err := caseInsensitiveHeaderValues(request.Header, name)
		if err != nil {
			return nil, true, err
		}
		for _, value := range values {
			if fieldValueHasToken(value, "close") {
				return values, true, nil
			}
		}
		return append([]string{"close"}, values...), true, nil
	default:
		return nil, false, nil
	}
}

func responseTransportFieldValues(response *http.Response, mode ResponseTransportMode, name string) ([]string, bool, error) {
	switch {
	case strings.EqualFold(name, "transfer-encoding"):
		values, err := responseTransferEncodingFieldValues(response, mode)
		return values, true, err
	case strings.EqualFold(name, "trailer"):
		values, err := responseTrailerFieldValues(response, mode)
		return values, true, err
	case strings.EqualFold(name, "connection"):
		values, err := responseConnectionFieldValues(response, mode)
		return values, true, err
	default:
		return nil, false, nil
	}
}

func receivedResponseCloseFieldIsExplicit(response *http.Response) bool {
	if response.ProtoMajor != 1 || response.ProtoMinor < 1 {
		return false
	}
	if response.ContentLength >= 0 || len(response.TransferEncoding) != 0 {
		return true
	}
	if response.Request != nil && response.Request.Method == http.MethodHead {
		return true
	}
	return !responseWriteBodyAllowed(response.StatusCode)
}

func trailerDeclarationFieldValues(trailer http.Header) ([]string, error) {
	keys := make([]string, 0, len(trailer))
	for key := range trailer {
		canonical := http.CanonicalHeaderKey(key)
		if !validHTTPToken(canonical) || strings.EqualFold(canonical, "transfer-encoding") ||
			strings.EqualFold(canonical, "trailer") || strings.EqualFold(canonical, "content-length") {
			return nil, errors.New("invalid Trailer declaration")
		}
		keys = append(keys, canonical)
	}
	sort.Strings(keys)
	return []string{strings.Join(keys, ",")}, nil
}

func transferEncodingFieldValues(encodings []string) ([]string, error) {
	if len(encodings) == 0 || len(encodings) == 1 && encodings[0] == "identity" {
		return nil, nil
	}
	if hasChunkedTransferEncoding(encodings) {
		return []string{"chunked"}, nil
	}
	return nil, errors.New("unsupported net/http transfer encoding")
}

func hasChunkedTransferEncoding(encodings []string) bool {
	return len(encodings) > 0 && encodings[0] == "chunked"
}

func requestUsesChunkedTransferEncoding(request *http.Request) bool {
	return request != nil && request.Body != nil && hasChunkedTransferEncoding(request.TransferEncoding)
}

func responseTransferEncodingFieldValues(response *http.Response, mode ResponseTransportMode) ([]string, error) {
	if response == nil {
		return nil, errors.New("response is unavailable")
	}
	received, receivedErr := receivedResponseTransferEncodingFieldValues(response.TransferEncoding)
	written, writeErr := writeResponseTransferEncodingFieldValues(response)
	switch mode {
	case ResponseTransportReceived:
		return received, receivedErr
	case ResponseTransportWrite:
		return written, writeErr
	default:
		return matchingResponseFieldValues(received, receivedErr, written, writeErr)
	}
}

func receivedResponseTransferEncodingFieldValues(encodings []string) ([]string, error) {
	if len(encodings) == 0 {
		return nil, nil
	}
	if len(encodings) == 1 && encodings[0] == "chunked" {
		return []string{"chunked"}, nil
	}
	return nil, errors.New("received response has impossible transfer coding state")
}

func writeResponseTransferEncodingFieldValues(response *http.Response) ([]string, error) {
	if responseUsesChunkedTransferEncoding(response) {
		return []string{"chunked"}, nil
	}
	if len(response.TransferEncoding) == 0 || len(response.TransferEncoding) == 1 && response.TransferEncoding[0] == "identity" ||
		response.Body == nil || response.Request == nil || response.Request.Method != http.MethodHead && !response.ProtoAtLeast(1, 1) {
		return nil, nil
	}
	return nil, errors.New("unsupported net/http transfer encoding")
}

func responseUsesChunkedTransferEncoding(response *http.Response) bool {
	if response == nil || !hasChunkedTransferEncoding(response.TransferEncoding) {
		return false
	}
	if response.Request != nil && response.Request.Method == http.MethodHead {
		return true
	}
	return response.Body != nil && response.ProtoAtLeast(1, 1)
}

func fieldValueHasToken(value, token string) bool {
	for candidate := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), token) {
			return true
		}
	}
	return false
}

func caseInsensitiveHeaderValues(header http.Header, name string) ([]string, error) {
	var values []string
	found := false
	for key, current := range header {
		if !strings.EqualFold(key, name) {
			continue
		}
		if found {
			return nil, errors.New("covered field has case-colliding map keys")
		}
		found = true
		values = current
	}
	return values, nil
}

func componentHTTPMajor(context MessageContext, related bool) int {
	if related || context.Request != nil {
		request, err := requestForComponent(context, related)
		if err != nil {
			return 0
		}
		return request.ProtoMajor
	}
	if context.Response != nil {
		return context.Response.ProtoMajor
	}
	return 0
}

func responseContentLengthFieldValues(response *http.Response, mode ResponseTransportMode) ([]string, error) {
	if response == nil {
		return nil, nil
	}
	received, receivedErr := receivedResponseContentLengthFieldValues(response)
	written, writeErr := writeResponseContentLengthFieldValues(response)
	switch mode {
	case ResponseTransportReceived:
		return received, receivedErr
	case ResponseTransportWrite:
		return written, writeErr
	default:
		return matchingResponseFieldValues(received, receivedErr, written, writeErr)
	}
}

func receivedResponseContentLengthFieldValues(response *http.Response) ([]string, error) {
	values, err := preservedResponseContentLengthFieldValues(response.Header)
	if err != nil || len(values) == 0 {
		return values, err
	}
	if len(response.TransferEncoding) != 0 {
		return nil, errors.New("received response has conflicting Content-Length and transfer coding")
	}
	contentLength, _ := strconv.ParseUint(strings.TrimSpace(values[0]), 10, 63)
	method := ""
	if response.Request != nil {
		method = response.Request.Method
	}
	if (responseWriteBodyAllowed(response.StatusCode) || method == http.MethodHead) &&
		(response.ContentLength < 0 || uint64(response.ContentLength) != contentLength) {
		return nil, errors.New("received response Content-Length disagrees with parsed transport state")
	}
	return values, nil
}

func writeResponseContentLengthFieldValues(response *http.Response) ([]string, error) {
	if responseUsesChunkedTransferEncoding(response) {
		return nil, nil
	}
	if response.ContentLength == 0 && response.Body != nil && response.Body != http.NoBody {
		return nil, errors.New("response content length depends on body probing")
	}

	method := ""
	if response.Request != nil {
		method = response.Request.Method
	}
	if method != http.MethodHead && response.Body == http.NoBody && response.ContentLength > 0 {
		return nil, errors.New("response body is shorter than its declared content length")
	}
	responseToHEAD := method == http.MethodHead
	contentLength := response.ContentLength
	effectiveTransferEncoding := response.TransferEncoding
	if !responseToHEAD {
		if !response.ProtoAtLeast(1, 1) {
			effectiveTransferEncoding = nil
		}
		if response.Body == nil {
			effectiveTransferEncoding = nil
			contentLength = 0
		}
	}
	if contentLength > 0 {
		return []string{strconv.FormatInt(contentLength, 10)}, nil
	}
	if contentLength < 0 {
		return nil, nil
	}
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return []string{"0"}, nil
	}
	if len(effectiveTransferEncoding) == 1 && effectiveTransferEncoding[0] == "identity" {
		switch method {
		case http.MethodGet, http.MethodHead:
		default:
			return []string{"0"}, nil
		}
	}
	if response.ContentLength == 0 && !hasChunkedTransferEncoding(response.TransferEncoding) && responseWriteBodyAllowed(response.StatusCode) {
		return []string{"0"}, nil
	}
	return nil, nil
}

func preservedResponseContentLengthFieldValues(header http.Header) ([]string, error) {
	values, err := caseInsensitiveHeaderValues(header, "content-length")
	if err != nil {
		return values, err
	}
	if len(values) == 0 {
		return values, err
	}
	if len(values) != 1 {
		return nil, errors.New("received response has ambiguous Content-Length")
	}
	if _, err := strconv.ParseUint(strings.TrimSpace(values[0]), 10, 63); err != nil {
		return nil, errors.New("received response has invalid Content-Length")
	}
	return values, nil
}

func responseTrailerFieldValues(response *http.Response, mode ResponseTransportMode) ([]string, error) {
	var received []string
	var receivedErr error
	if len(response.Trailer) != 0 {
		receivedErr = errors.New("received Trailer declaration order is unavailable")
	}
	var written []string
	var writeErr error
	if responseUsesChunkedTransferEncoding(response) && len(response.Trailer) != 0 {
		written, writeErr = trailerDeclarationFieldValues(response.Trailer)
	}
	switch mode {
	case ResponseTransportReceived:
		return received, receivedErr
	case ResponseTransportWrite:
		return written, writeErr
	default:
		return matchingResponseFieldValues(received, receivedErr, written, writeErr)
	}
}

func responseConnectionFieldValues(response *http.Response, mode ResponseTransportMode) ([]string, error) {
	receivedClose := false
	if response.Close {
		receivedClose = receivedResponseCloseFieldIsExplicit(response)
	}
	received, receivedErr := responseCloseFieldValues(response, receivedClose)
	written, writeErr := responseCloseFieldValues(response, responseWriteClosesConnection(response))
	switch mode {
	case ResponseTransportReceived:
		return received, receivedErr
	case ResponseTransportWrite:
		return written, writeErr
	default:
		return matchingResponseFieldValues(received, receivedErr, written, writeErr)
	}
}

func responseWriteClosesConnection(response *http.Response) bool {
	return response.Close || response.ContentLength == -1 && response.ProtoAtLeast(1, 1) &&
		!hasChunkedTransferEncoding(response.TransferEncoding) && !response.Uncompressed
}

func responseCloseFieldValues(response *http.Response, synthesizeClose bool) ([]string, error) {
	values, err := caseInsensitiveHeaderValues(response.Header, "connection")
	if err != nil {
		return nil, err
	}
	if !synthesizeClose {
		return values, nil
	}
	for _, value := range values {
		if fieldValueHasToken(value, "close") {
			return values, nil
		}
	}
	return append([]string{"close"}, values...), nil
}

func matchingResponseFieldValues(received []string, receivedErr error, written []string, writeErr error) ([]string, error) {
	if receivedErr != nil || writeErr != nil || !sameFieldValues(received, written) {
		return nil, errors.New("response transport mode is required for ambiguous field identity")
	}
	return received, nil
}

func sameFieldValues(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func responseWriteBodyAllowed(status int) bool {
	return status < 100 || status > 199 && status != http.StatusNoContent && status != http.StatusNotModified
}

func requestContentLength(request *http.Request) (int64, bool) {
	if request == nil {
		return 0, false
	}
	if requestUsesChunkedTransferEncoding(request) {
		return 0, false
	}
	identityTransfer := len(request.TransferEncoding) == 1 && request.TransferEncoding[0] == "identity"
	if request.Body == nil {
		if request.ContentLength != 0 {
			return 0, false
		}
	} else if request.Body != http.NoBody {
		if request.ContentLength > 0 {
			return request.ContentLength, true
		}
		return 0, false
	}
	method := request.Method
	if method == "" {
		method = http.MethodGet
	}
	if method == http.MethodGet || method == http.MethodHead {
		return 0, false
	}
	return 0, identityTransfer || method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
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
	authority := request.Host
	if authority == "" {
		authority = request.URL.Host
	}
	requestTarget := request.RequestURI
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
	} else if requestTarget == "" {
		requestTarget = request.URL.RequestURI()
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
	switch request {
	case context.Request, context.RelatedRequest:
		return context.ExternalRequest
	}
	return nil
}

func normalizeAuthority(authority, scheme string) (string, error) {
	authority = removeIPv6Zone(authority)
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

func removeIPv6Zone(host string) string {
	if !strings.HasPrefix(host, "[") {
		return host
	}
	closing := strings.IndexByte(host, ']')
	if closing == -1 {
		return host
	}
	zone := strings.LastIndexByte(host[:closing], '%')
	if zone == -1 {
		return host
	}
	return host[:zone] + host[closing:]
}

func validNetHTTPHostHeader(host string) bool {
	const allowed = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!$%&()*+,-.:;='[]_~"
	for index := 0; index < len(host); index++ {
		if !strings.ContainsRune(allowed, rune(host[index])) {
			return false
		}
	}
	return true
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
		item, itemErr := unmarshalStructuredItem(normalizeStructuredFieldOWS(values))
		value, err = item, itemErr
	default:
		return "", errors.New("unknown structured field type")
	}
	if err != nil {
		return "", errors.New("malformed structured field")
	}
	if !isRFC8941StructuredField(value) {
		return "", errors.New("structured field uses RFC 9651-only value")
	}
	// Successful parsing plus the RFC 8941 type guard makes this serialization
	// total; returning its error directly still fails closed if the dependency
	// ever violates that contract.
	return marshalRFC8941(value)
}

func isRFC8941StructuredField(value httpsfv.StructuredFieldValue) bool {
	switch field := value.(type) {
	case httpsfv.Item:
		return isRFC8941Item(field)
	case httpsfv.InnerList:
		return isRFC8941Member(field)
	case httpsfv.List:
		for _, member := range field {
			if !isRFC8941Member(member) {
				return false
			}
		}
		return true
	case *httpsfv.Dictionary:
		for _, name := range field.Names() {
			member, _ := field.Get(name)
			if !isRFC8941Member(member) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isRFC8941Member(member httpsfv.Member) bool {
	switch value := member.(type) {
	case httpsfv.Item:
		return isRFC8941Item(value)
	case httpsfv.InnerList:
		if !isRFC8941Parameters(value.Params) {
			return false
		}
		for _, item := range value.Items {
			if !isRFC8941Item(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isRFC8941Item(item httpsfv.Item) bool {
	return isRFC8941BareItem(item.Value) && isRFC8941Parameters(item.Params)
}

func isRFC8941Parameters(parameters *httpsfv.Params) bool {
	if parameters == nil {
		return false
	}
	for _, name := range parameters.Names() {
		value, _ := parameters.Get(name)
		if !isRFC8941BareItem(value) {
			return false
		}
	}
	return true
}

func isRFC8941BareItem(value any) bool {
	switch value.(type) {
	case bool, string, int64, float64, []byte, httpsfv.Token:
		return true
	default:
		return false
	}
}

func marshalMember(member httpsfv.Member) string {
	dictionary := httpsfv.NewDictionary()
	dictionary.Add("x", member)
	serialized, _ := marshalRFC8941(dictionary)
	separator := strings.IndexByte(serialized, '=')
	if separator == -1 {
		return "?1" + strings.TrimPrefix(serialized, "x")
	}
	return serialized[separator+1:]
}

func serializeComponentIdentifier(component ComponentIdentifier) (string, error) {
	if !validComponentName(component.Name) {
		return "", errors.New("invalid component identifier")
	}
	if !validParametersForSerialization(component.Parameters) {
		return "", errors.New("invalid component identifier")
	}
	item := httpsfv.NewItem(component.Name)
	addParameters(item.Params, component.Parameters)
	return marshalRFC8941(item)
}

func serializeSignatureParameters(input SignatureInput) (string, error) {
	if !validParametersForSerialization(input.Parameters) {
		return "", errors.New("invalid signature parameters")
	}
	items := make([]httpsfv.Item, len(input.Components))
	for index, component := range input.Components {
		if !validComponentName(component.Name) {
			return "", errors.New("invalid component identifier")
		}
		if !validParametersForSerialization(component.Parameters) {
			return "", errors.New("invalid component identifier")
		}
		items[index] = httpsfv.NewItem(component.Name)
		addParameters(items[index].Params, component.Parameters)
	}
	parameters := httpsfv.NewParams()
	addParameters(parameters, input.Parameters)
	return marshalRFC8941(httpsfv.InnerList{Items: items, Params: parameters})
}

func queryParameter(rawQuery, encodedName string, maxBytes int) (string, error) {
	if rawQuery == "" {
		return "", errors.New("named query parameter is absent")
	}
	if len(rawQuery) > maxBytes/3 {
		return "", ErrSignatureBaseLimit
	}
	var found string
	foundCount := 0
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair != "" {
			name, value, hasValue := strings.Cut(pair, "=")
			if !hasValue {
				value = ""
			}
			decodedName := decodeFormComponent(name)
			if formPercentEncode(decodedName) == encodedName {
				decodedValue := decodeFormComponent(value)
				found = formPercentEncode(decodedValue)
				foundCount++
			}
		}
	}
	if foundCount != 1 {
		return "", errors.New("named query parameter is absent or repeated")
	}
	if len(found) > maxBytes {
		return "", ErrSignatureBaseLimit
	}
	return found, nil
}

func decodeFormComponent(value string) string {
	decoded := make([]byte, 0, len(value))
	remaining := value
	for remaining != "" {
		switch {
		case remaining[0] == '+':
			decoded = append(decoded, ' ')
			remaining = remaining[1:]
		case remaining[0] == '%' && len(remaining) >= 3:
			high, highOK := hexValue(remaining[1])
			low, lowOK := hexValue(remaining[2])
			if highOK && lowOK {
				decoded = append(decoded, high<<4|low)
				remaining = remaining[3:]
			} else {
				decoded = append(decoded, remaining[0])
				remaining = remaining[1:]
			}
		default:
			decoded = append(decoded, remaining[0])
			remaining = remaining[1:]
		}
	}
	return decodeUTF8WithReplacement(decoded)
}

func decodeUTF8WithReplacement(value []byte) string {
	var builder strings.Builder
	remaining := value
	for len(remaining) != 0 {
		if remaining[0] < utf8.RuneSelf {
			builder.WriteByte(remaining[0])
			remaining = remaining[1:]
		} else {
			width, secondMin, secondMax := utf8Sequence(remaining[0])
			switch width {
			case 0:
				builder.WriteRune(utf8.RuneError)
				remaining = remaining[1:]
			default:
				available := min(width, len(remaining))
				consumed := 1
				valid := available == width
				for offset, character := range remaining[1:available] {
					minimum, maximum := byte(0x80), byte(0xbf)
					if offset == 0 {
						minimum, maximum = secondMin, secondMax
					}
					if character < minimum || character > maximum {
						valid = false
						break
					}
					consumed = offset + 2
				}
				if valid {
					runeValue, _ := utf8.DecodeRune(remaining[:width])
					builder.WriteRune(runeValue)
					remaining = remaining[width:]
				} else {
					builder.WriteRune(utf8.RuneError)
					remaining = remaining[consumed:]
				}
			}
		}
	}
	return builder.String()
}

func utf8Sequence(first byte) (width int, secondMin, secondMax byte) {
	switch {
	case first >= 0xc2 && first <= 0xdf:
		return 2, 0x80, 0xbf
	case first == 0xe0:
		return 3, 0xa0, 0xbf
	case first >= 0xe1 && first <= 0xec || first >= 0xee && first <= 0xef:
		return 3, 0x80, 0xbf
	case first == 0xed:
		return 3, 0x80, 0x9f
	case first == 0xf0:
		return 4, 0x90, 0xbf
	case first >= 0xf1 && first <= 0xf3:
		return 4, 0x80, 0xbf
	case first == 0xf4:
		return 4, 0x80, 0x8f
	default:
		return 0, 0, 0
	}
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
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

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
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
