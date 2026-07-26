package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	// ErrInvalidRequestSpec indicates invalid request construction policy.
	ErrInvalidRequestSpec = errors.New("invalid request specification")
	// ErrInvalidURL indicates an unsafe or malformed base URL or reference.
	ErrInvalidURL = errors.New("invalid request URL")
	// ErrInvalidHeader indicates an invalid HTTP field name or value.
	ErrInvalidHeader = errors.New("invalid HTTP header")
	// ErrInvalidTrailer indicates an invalid HTTP trailer name, value, or use.
	ErrInvalidTrailer = errors.New("invalid HTTP trailer")
	// ErrInvalidQuery indicates an invalid query name, value, or encoding.
	ErrInvalidQuery = errors.New("invalid HTTP query")
)

// RequestLayer defines deterministic precedence for request metadata. Higher
// layers replace values from lower layers.
type RequestLayer uint8

const (
	// LayerClient contains client-wide defaults.
	LayerClient RequestLayer = iota
	// LayerEndpoint contains endpoint-specific defaults.
	LayerEndpoint
	// LayerRequest contains logical-operation values.
	LayerRequest
	// LayerAuthentication contains authentication decorator values.
	LayerAuthentication
	// LayerSigning contains request-signing values.
	LayerSigning
	// LayerOneShot contains values for one physical request build.
	LayerOneShot
	requestLayerCount
)

// String returns the stable policy name for a request layer.
func (layer RequestLayer) String() string {
	switch layer {
	case LayerClient:
		return "client"
	case LayerEndpoint:
		return "endpoint"
	case LayerRequest:
		return "request"
	case LayerAuthentication:
		return "authentication"
	case LayerSigning:
		return "signing"
	case LayerOneShot:
		return "one-shot"
	default:
		return fmt.Sprintf("layer(%d)", layer)
	}
}

// QueryStyle identifies a supported query array serialization style.
type QueryStyle uint8

const (
	// QueryRepeated emits one key-value pair per item.
	QueryRepeated QueryStyle = iota
	// QueryCommaDelimited emits one comma-delimited value.
	QueryCommaDelimited
	// QuerySpaceDelimited emits one space-delimited value.
	QuerySpaceDelimited
	// QueryPipeDelimited emits one pipe-delimited value.
	QueryPipeDelimited
	queryDeepObject
	queryCustom
	queryNull
)

// QueryPart is one structurally encoded custom query field. HasValue false
// emits a bare field name and ignores Value.
type QueryPart struct {
	Name     string
	Value    string
	HasValue bool
}

// QueryEncoder serializes one named custom query value into structured parts.
// Implementations must be deterministic and safe for concurrent use.
type QueryEncoder interface {
	EncodeQuery(name string) ([]QueryPart, error)
}

// QueryEncoderFunc adapts a function to QueryEncoder.
type QueryEncoderFunc func(name string) ([]QueryPart, error)

// EncodeQuery implements QueryEncoder.
func (function QueryEncoderFunc) EncodeQuery(name string) ([]QueryPart, error) {
	return function(name)
}

// QueryEncodingError reports a custom encoder failure without exposing query
// values in the rendered error.
type QueryEncodingError struct {
	Parameter string
	Cause     error
}

// Error implements error.
func (err *QueryEncodingError) Error() string {
	return fmt.Sprintf("query parameter %q encoding failed", err.Parameter)
}

// Unwrap returns the custom encoder failure.
func (err *QueryEncodingError) Unwrap() error {
	return err.Cause
}

// QueryValue is an immutable query serialization instruction. Use a query
// constructor rather than its zero value.
type QueryValue struct {
	style  QueryStyle
	values []string
	object map[string]string
	custom QueryEncoder
	valid  bool
}

// RepeatedQuery serializes every value as a separate key-value pair. Value
// order is preserved while parameter names are ordered canonically.
func RepeatedQuery(values ...string) QueryValue {
	return QueryValue{
		style:  QueryRepeated,
		values: append([]string(nil), values...),
		valid:  true,
	}
}

// QueryValues serializes values with any exported query array style.
func QueryValues(style QueryStyle, values ...string) (QueryValue, error) {
	if style > QueryPipeDelimited {
		return QueryValue{}, fmt.Errorf("%w: style is not an exported array style", ErrInvalidQuery)
	}

	value := QueryValue{
		style:  style,
		values: append([]string(nil), values...),
		valid:  true,
	}
	if err := validateQueryValue(value); err != nil {
		return QueryValue{}, err
	}

	return value, nil
}

// DeepObjectQuery serializes fields as name[field]=value. The input map is
// copied and field names are ordered canonically at build time.
func DeepObjectQuery(fields map[string]string) QueryValue {
	object := make(map[string]string, len(fields))
	for name, value := range fields {
		object[name] = value
	}

	return QueryValue{style: queryDeepObject, object: object, valid: true}
}

// NullQuery serializes an explicitly present null value as a bare field name.
func NullQuery() QueryValue {
	return QueryValue{style: queryNull, valid: true}
}

// CustomQuery creates a structurally escaped custom query value.
func CustomQuery(encoder QueryEncoder) (QueryValue, error) {
	if nilLike(encoder) {
		return QueryValue{}, fmt.Errorf("%w: custom encoder is nil", ErrInvalidQuery)
	}

	return QueryValue{style: queryCustom, custom: encoder, valid: true}, nil
}

// RequestSpec is an immutable reusable request description. It resolves one
// relative reference against a fixed base URL and layers request metadata
// without replacing standard HTTP request types.
type RequestSpec struct {
	target *url.URL
	layers [requestLayerCount]requestSpecLayer
	body   RequestBody
}

// WithBody returns a spec using body. Replayable bodies can build any number of
// requests; streaming bodies are explicitly one-shot.
func (spec RequestSpec) WithBody(body RequestBody) (RequestSpec, error) {
	if nilLike(body) {
		return RequestSpec{}, fmt.Errorf("%w: body is nil", ErrInvalidBody)
	}
	if err := validateBodyMetadata(body.ContentType(), body.ContentLength()); err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.body = body

	return clone, nil
}

// WithoutBody returns a spec without a request body.
func (spec RequestSpec) WithoutBody() RequestSpec {
	clone := spec.clone()
	clone.body = nil

	return clone
}

type requestSpecLayer struct {
	headers  map[string]headerInstruction
	trailers map[string]headerInstruction
	query    map[string]queryInstruction
}

type headerInstruction struct {
	values []string
	remove bool
}

type queryInstruction struct {
	value  QueryValue
	remove bool
}

// NewRequestSpec parses baseURL and resolves reference using RFC 3986 URL
// resolution. The base must be an absolute HTTP(S) URL without user
// information. The reference must not select another scheme or authority.
func NewRequestSpec(baseURL string, reference string) (RequestSpec, error) {
	base, err := url.Parse(baseURL)
	if err != nil || !validBaseURL(base) {
		return RequestSpec{}, fmt.Errorf("%w: base must be an absolute HTTP(S) URL without user information", ErrInvalidURL)
	}

	relative, err := url.Parse(reference)
	if err != nil || !validRelativeReference(relative) {
		return RequestSpec{}, fmt.Errorf("%w: reference must not contain a scheme, authority, or user information", ErrInvalidURL)
	}

	target := base.ResolveReference(relative)
	if !sameOrigin(base, target) || relative.IsAbs() || relative.Host != "" {
		return RequestSpec{}, fmt.Errorf("%w: reference changed the base origin", ErrInvalidURL)
	}

	return RequestSpec{target: cloneURL(target)}, nil
}

// WithHeader returns a spec with name replaced at layer. Higher layers still
// take precedence. Values are copied and remain separate header field values.
func (spec RequestSpec) WithHeader(layer RequestLayer, name string, values ...string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateHeader(name, values)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].headers[canonicalName] = headerInstruction{
		values: append([]string(nil), values...),
	}

	return clone, nil
}

// AddHeader returns a spec with values appended at layer. It does not
// comma-fold values because several standard fields prohibit folding.
func (spec RequestSpec) AddHeader(layer RequestLayer, name string, values ...string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateHeader(name, values)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	instruction := clone.layers[layer].headers[canonicalName]
	if instruction.remove {
		instruction = headerInstruction{}
	}
	instruction.values = append(instruction.values, values...)
	clone.layers[layer].headers[canonicalName] = instruction

	return clone, nil
}

// WithoutHeader returns a spec that removes an inherited header at layer.
func (spec RequestSpec) WithoutHeader(layer RequestLayer, name string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateHeaderName(name)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].headers[canonicalName] = headerInstruction{remove: true}

	return clone, nil
}

// WithTrailer returns a spec with trailer name replaced at layer. Trailer
// values are snapshotted and require a request body at build time.
func (spec RequestSpec) WithTrailer(layer RequestLayer, name string, values ...string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateTrailer(name, values)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].trailers[canonicalName] = headerInstruction{
		values: append([]string(nil), values...),
	}
	return clone, nil
}

// AddTrailer returns a spec with values appended to a trailer at layer.
func (spec RequestSpec) AddTrailer(layer RequestLayer, name string, values ...string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateTrailer(name, values)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	instruction := clone.layers[layer].trailers[canonicalName]
	if instruction.remove {
		instruction = headerInstruction{}
	}
	instruction.values = append(instruction.values, values...)
	clone.layers[layer].trailers[canonicalName] = instruction
	return clone, nil
}

// WithoutTrailer returns a spec that removes an inherited trailer at layer.
func (spec RequestSpec) WithoutTrailer(layer RequestLayer, name string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	canonicalName, err := validateTrailerName(name)
	if err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].trailers[canonicalName] = headerInstruction{remove: true}
	return clone, nil
}

// WithQuery returns a spec with name replaced at layer.
func (spec RequestSpec) WithQuery(layer RequestLayer, name string, value QueryValue) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	if err := validateQueryName(name); err != nil {
		return RequestSpec{}, err
	}
	if err := validateQueryValue(value); err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].query[name] = queryInstruction{value: cloneQueryValue(value)}

	return clone, nil
}

// WithoutQuery returns a spec that removes an inherited query parameter at
// layer, including a parameter supplied by the relative reference.
func (spec RequestSpec) WithoutQuery(layer RequestLayer, name string) (RequestSpec, error) {
	if err := validateLayer(layer); err != nil {
		return RequestSpec{}, err
	}
	if err := validateQueryName(name); err != nil {
		return RequestSpec{}, err
	}

	clone := spec.clone()
	clone.ensureLayer(layer)
	clone.layers[layer].query[name] = queryInstruction{remove: true}

	return clone, nil
}

// Build creates an independent standard HTTP request. Mutable headers and the
// URL never alias the spec or another request built from it.
func (spec RequestSpec) Build(ctx context.Context, method string) (*http.Request, error) {
	if spec.target == nil {
		return nil, fmt.Errorf("%w: zero request specification", ErrInvalidRequestSpec)
	}

	target := cloneURL(spec.target)
	query, err := queryFromURL(target)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	trailers := make(http.Header)
	if spec.body != nil && spec.body.ContentType() != "" {
		headers.Set("Content-Type", spec.body.ContentType())
	}

	for layer := LayerClient; layer < requestLayerCount; layer++ {
		applyHeaders(headers, spec.layers[layer].headers)
		applyHeaders(trailers, spec.layers[layer].trailers)
		applyQuery(query, spec.layers[layer].query)
	}
	if len(trailers) > 0 && spec.body == nil {
		return nil, fmt.Errorf("%w: trailers require a request body", ErrInvalidTrailer)
	}
	target.RawQuery, err = encodeQuery(query)
	if err != nil {
		return nil, err
	}
	target.ForceQuery = false

	var openedBody io.ReadCloser
	if spec.body != nil {
		if err := validateBodyMetadata(spec.body.ContentType(), spec.body.ContentLength()); err != nil {
			return nil, err
		}
		openedBody, err = openRequestBody(spec.body)
		if err != nil {
			return nil, err
		}
	}

	request, err := http.NewRequestWithContext(ctx, method, target.String(), openedBody)
	if err != nil {
		if openedBody != nil {
			_ = openedBody.Close()
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequestSpec, err)
	}
	request.URL = target
	request.Header = headers
	if len(trailers) > 0 {
		request.Trailer = trailers
	}
	if spec.body != nil {
		request.ContentLength = spec.body.ContentLength()
		if len(trailers) > 0 {
			request.ContentLength = -1
		}
		if spec.body.Replayable() {
			request.GetBody = func() (io.ReadCloser, error) {
				return openRequestBody(spec.body)
			}
		} else {
			request.GetBody = nil
		}
	}

	return request, nil
}

func validBaseURL(candidate *url.URL) bool {
	if candidate == nil || candidate.Opaque != "" || candidate.User != nil {
		return false
	}
	if candidate.Host == "" || !candidate.IsAbs() {
		return false
	}

	return strings.EqualFold(candidate.Scheme, "http") || strings.EqualFold(candidate.Scheme, "https")
}

func validRelativeReference(candidate *url.URL) bool {
	return candidate != nil && candidate.User == nil && candidate.Opaque == ""
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}

	clone := *source
	if source.User != nil {
		user := *source.User
		clone.User = &user
	}

	return &clone
}

func (spec RequestSpec) clone() RequestSpec {
	clone := RequestSpec{target: cloneURL(spec.target), body: spec.body}
	for layer := LayerClient; layer < requestLayerCount; layer++ {
		if spec.layers[layer].headers != nil {
			clone.layers[layer].headers = make(map[string]headerInstruction, len(spec.layers[layer].headers))
			for name, instruction := range spec.layers[layer].headers {
				instruction.values = append([]string(nil), instruction.values...)
				clone.layers[layer].headers[name] = instruction
			}
		}
		if spec.layers[layer].trailers != nil {
			clone.layers[layer].trailers = make(map[string]headerInstruction, len(spec.layers[layer].trailers))
			for name, instruction := range spec.layers[layer].trailers {
				instruction.values = append([]string(nil), instruction.values...)
				clone.layers[layer].trailers[name] = instruction
			}
		}
		if spec.layers[layer].query != nil {
			clone.layers[layer].query = make(map[string]queryInstruction, len(spec.layers[layer].query))
			for name, instruction := range spec.layers[layer].query {
				instruction.value = cloneQueryValue(instruction.value)
				clone.layers[layer].query[name] = instruction
			}
		}
	}

	return clone
}

func openRequestBody(body RequestBody) (io.ReadCloser, error) {
	opened, err := body.Open()
	if err != nil {
		return nil, &BodyOpenError{Cause: err}
	}
	if opened == nil {
		return nil, fmt.Errorf("%w: body opener returned a nil reader", ErrInvalidBody)
	}

	return opened, nil
}

func (spec *RequestSpec) ensureLayer(layer RequestLayer) {
	if spec.layers[layer].headers == nil {
		spec.layers[layer].headers = make(map[string]headerInstruction)
	}
	if spec.layers[layer].trailers == nil {
		spec.layers[layer].trailers = make(map[string]headerInstruction)
	}
	if spec.layers[layer].query == nil {
		spec.layers[layer].query = make(map[string]queryInstruction)
	}
}

func cloneQueryValue(value QueryValue) QueryValue {
	value.values = append([]string(nil), value.values...)
	if value.object != nil {
		object := make(map[string]string, len(value.object))
		for name, item := range value.object {
			object[name] = item
		}
		value.object = object
	}

	return value
}

func validateLayer(layer RequestLayer) error {
	if layer >= requestLayerCount {
		return fmt.Errorf("%w: unknown request layer %d", ErrInvalidRequestSpec, layer)
	}

	return nil
}

func validateHeader(name string, values []string) (string, error) {
	canonicalName, err := validateHeaderName(name)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", fmt.Errorf("%w: header requires at least one value", ErrInvalidHeader)
	}
	for _, value := range values {
		if !validHeaderValue(value) {
			return "", fmt.Errorf("%w: field value contains prohibited bytes", ErrInvalidHeader)
		}
	}

	return canonicalName, nil
}

func validateHeaderName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: field name is empty", ErrInvalidHeader)
	}
	for index := 0; index < len(name); index++ {
		if !headerTokenByte(name[index]) {
			return "", fmt.Errorf("%w: field name is not an HTTP token", ErrInvalidHeader)
		}
	}

	return http.CanonicalHeaderKey(name), nil
}

func validateTrailer(name string, values []string) (string, error) {
	canonicalName, err := validateTrailerName(name)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", fmt.Errorf("%w: trailer requires at least one value", ErrInvalidTrailer)
	}
	for _, value := range values {
		if !validHeaderValue(value) {
			return "", fmt.Errorf("%w: field value contains prohibited bytes", ErrInvalidTrailer)
		}
	}
	return canonicalName, nil
}

func validateTrailerName(name string) (string, error) {
	canonicalName, err := validateHeaderName(name)
	if err != nil {
		return "", fmt.Errorf("%w: field name is not an HTTP token", ErrInvalidTrailer)
	}
	switch canonicalName {
	case "Authorization", "Content-Encoding", "Content-Length", "Content-Range",
		"Content-Type", "Cookie", "Host", "Proxy-Authorization", "Set-Cookie",
		"Te", "Trailer", "Transfer-Encoding":
		return "", fmt.Errorf("%w: field is prohibited in trailers", ErrInvalidTrailer)
	}
	return canonicalName, nil
}

func headerTokenByte(value byte) bool {
	if value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' {
		return true
	}

	return strings.ContainsRune("!#$%&'*+-.^_`|~", rune(value))
}

func validHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '\t' {
			continue
		}
		if character < 0x20 || character == 0x7f {
			return false
		}
	}

	return true
}

func validateQueryName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("%w: parameter name is empty or invalid UTF-8", ErrInvalidQuery)
	}

	return nil
}

func validateQueryValue(value QueryValue) error {
	if !value.valid || value.style > queryNull {
		return fmt.Errorf("%w: unknown serialization style", ErrInvalidQuery)
	}
	for _, item := range value.values {
		if !utf8.ValidString(item) {
			return fmt.Errorf("%w: parameter value is invalid UTF-8", ErrInvalidQuery)
		}
	}
	if value.style == queryDeepObject {
		for name, item := range value.object {
			if err := validateQueryName(name); err != nil {
				return err
			}
			if !utf8.ValidString(item) {
				return fmt.Errorf("%w: deep-object value is invalid UTF-8", ErrInvalidQuery)
			}
		}
	}
	if value.style == queryCustom && value.custom == nil {
		return fmt.Errorf("%w: custom encoder is nil", ErrInvalidQuery)
	}

	return nil
}

func queryFromURL(target *url.URL) (map[string]QueryValue, error) {
	values, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: relative reference query is malformed", ErrInvalidQuery)
	}

	query := make(map[string]QueryValue, len(values))
	for name, items := range values {
		if err := validateQueryName(name); err != nil {
			return nil, err
		}
		query[name] = RepeatedQuery(items...)
	}

	return query, nil
}

func applyHeaders(target http.Header, instructions map[string]headerInstruction) {
	for name, instruction := range instructions {
		if instruction.remove {
			target.Del(name)
			continue
		}
		target[name] = append([]string(nil), instruction.values...)
	}
}

func applyQuery(target map[string]QueryValue, instructions map[string]queryInstruction) {
	for name, instruction := range instructions {
		if instruction.remove {
			delete(target, name)
			continue
		}
		target[name] = cloneQueryValue(instruction.value)
	}
}

func encodeQuery(query map[string]QueryValue) (string, error) {
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		value := query[name]
		if err := validateQueryValue(value); err != nil {
			return "", err
		}
		encoded, err := encodeQueryValue(name, value)
		if err != nil {
			return "", err
		}
		parts = append(parts, encoded...)
	}

	return strings.Join(parts, "&"), nil
}

func encodeQueryValue(name string, value QueryValue) ([]string, error) {
	switch value.style {
	case QueryRepeated:
		parts := make([]string, 0, len(value.values))
		for _, item := range value.values {
			parts = append(parts, encodeQueryPart(QueryPart{Name: name, Value: item, HasValue: true}))
		}

		return parts, nil
	case QueryCommaDelimited, QuerySpaceDelimited, QueryPipeDelimited:
		if len(value.values) == 0 {
			return nil, nil
		}
		delimiter := map[QueryStyle]string{
			QueryCommaDelimited: ",",
			QuerySpaceDelimited: " ",
			QueryPipeDelimited:  "|",
		}[value.style]

		return []string{encodeQueryPart(QueryPart{
			Name:     name,
			Value:    strings.Join(value.values, delimiter),
			HasValue: true,
		})}, nil
	case queryDeepObject:
		fields := make([]string, 0, len(value.object))
		for field := range value.object {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			parts = append(parts, encodeQueryPart(QueryPart{
				Name:     name + "[" + field + "]",
				Value:    value.object[field],
				HasValue: true,
			}))
		}

		return parts, nil
	case queryCustom:
		customParts, err := value.custom.EncodeQuery(name)
		if err != nil {
			return nil, &QueryEncodingError{Parameter: name, Cause: err}
		}
		parts := make([]string, 0, len(customParts))
		for _, part := range customParts {
			if err := validateQueryName(part.Name); err != nil {
				return nil, err
			}
			if part.HasValue && !utf8.ValidString(part.Value) {
				return nil, fmt.Errorf("%w: custom value is invalid UTF-8", ErrInvalidQuery)
			}
			parts = append(parts, encodeQueryPart(part))
		}

		return parts, nil
	case queryNull:
		return []string{escapeQuery(name)}, nil
	default:
		return nil, fmt.Errorf("%w: unknown serialization style", ErrInvalidQuery)
	}
}

func encodeQueryPart(part QueryPart) string {
	if !part.HasValue {
		return escapeQuery(part.Name)
	}

	return escapeQuery(part.Name) + "=" + escapeQuery(part.Value)
}

func escapeQuery(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}
