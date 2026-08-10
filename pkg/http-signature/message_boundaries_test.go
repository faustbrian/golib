package httpsignature

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/dunglas/httpsfv"
)

func TestSignatureBaseRejectsCallerBuiltBoundaryValues(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	for _, message := range []MessageContext{{}, {Request: request, Response: &http.Response{StatusCode: 200}}} {
		if _, err := CreateSignatureBase(message, SignatureInput{}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("target cardinality error = %v", err)
		}
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}, {Name: "@method"}}}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("duplicate component error = %v", err)
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{Parameters: []Parameter{{Name: "UPPER", Value: true}}}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("invalid signature parameters error = %v", err)
	}
}

func TestComponentParameterAndResolutionBoundaries(t *testing.T) {
	t.Parallel()

	for _, parameters := range [][]Parameter{
		{{Name: "sf", Value: false}},
		{{Name: "key", Value: true}},
		{{Name: "name", Value: true}},
		{{Name: "unknown", Value: true}},
		{{Name: "bs", Value: true}, {Name: "sf", Value: true}},
		{{Name: "bs", Value: true}, {Name: "key", Value: "x"}},
	} {
		if _, err := componentParameterSet(parameters); err == nil {
			t.Fatalf("componentParameterSet(%#v) succeeded", parameters)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	for _, test := range []struct {
		name       string
		component  string
		parameters componentParameters
		context    MessageContext
	}{
		{name: "derived field parameter", component: "@method", parameters: componentParameters{sf: true}, context: MessageContext{Request: request}},
		{name: "name outside query", component: "@method", parameters: componentParameters{hasName: true}, context: MessageContext{Request: request}},
		{name: "missing request", component: "@method", context: MessageContext{Response: &http.Response{StatusCode: 200}}},
		{name: "query name required", component: "@query-param", context: MessageContext{Request: request}},
		{name: "status req", component: "@status", parameters: componentParameters{req: true}, context: MessageContext{Response: &http.Response{StatusCode: 200}, RelatedRequest: request}},
		{name: "status invalid", component: "@status", context: MessageContext{Response: &http.Response{StatusCode: 99}}},
		{name: "unknown", component: "@unknown", context: MessageContext{Request: request}},
	} {
		if _, err := resolveDerived(test.context, test.component, test.parameters); err == nil {
			t.Fatalf("resolveDerived(%s) succeeded", test.name)
		}
	}
	if _, err := resolveComponent(MessageContext{Request: request}, ComponentIdentifier{Name: "@signature-params"}); err == nil {
		t.Fatal("explicit @signature-params succeeded")
	}
}

func TestFieldResolutionAndHeaderSelectionBoundaries(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	request.Header = http.Header{"X-Test": []string{"value"}}
	request.Trailer = http.Header{"X-Trailer": []string{"trailer"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"X-Test": []string{"response"}}, Trailer: http.Header{"X-Trailer": []string{"response trailer"}}}
	context := MessageContext{Response: response, RelatedRequest: request}
	for _, test := range []struct {
		related bool
		trailer bool
		want    string
	}{
		{related: true, want: "value"},
		{related: true, trailer: true, want: "trailer"},
		{want: "response"},
		{trailer: true, want: "response trailer"},
	} {
		header, err := headerForComponent(context, test.related, test.trailer)
		if err != nil || (header.Get("X-Test") != test.want && header.Get("X-Trailer") != test.want) {
			t.Fatalf("headerForComponent(%t,%t) = %#v, %v", test.related, test.trailer, header, err)
		}
	}
	if _, err := headerForComponent(MessageContext{Response: response}, true, false); err == nil {
		t.Fatal("related header without request succeeded")
	}
	if _, err := resolveField(MessageContext{Response: response}, "x", componentParameters{req: true}); err == nil {
		t.Fatal("related field without request succeeded")
	}
	hostRequest := &http.Request{Host: "example.com", Header: make(http.Header)}
	if value, err := resolveField(MessageContext{Request: hostRequest}, "host", componentParameters{}); err != nil || value != "example.com" {
		t.Fatalf("Host fallback = %q, %v", value, err)
	}
	for _, test := range []struct {
		name       string
		parameters componentParameters
		context    MessageContext
	}{
		{name: "name", parameters: componentParameters{hasName: true}, context: MessageContext{Request: request}},
		{name: "missing", context: MessageContext{Request: request}},
		{name: "binary newline", parameters: componentParameters{bs: true}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"bad\nvalue"}}}}},
		{name: "non ascii", context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"\u0080"}}}}},
		{name: "dictionary malformed", parameters: componentParameters{hasKey: true, key: "x"}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"Bad"}}}}},
		{name: "dictionary absent", parameters: componentParameters{hasKey: true, key: "missing"}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"x=1"}}}}},
		{name: "structured unknown", parameters: componentParameters{sf: true}, context: MessageContext{Request: &http.Request{Header: http.Header{"X": []string{"1"}}}}},
	} {
		if _, err := resolveField(test.context, "x", test.parameters); err == nil {
			t.Fatalf("resolveField(%s) succeeded", test.name)
		}
	}
}

func TestRequestPartsAndAuthorityBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := requestParts(nil, nil); err == nil {
		t.Fatal("requestParts(nil) succeeded")
	}
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/", Host: "example.com"}
	if _, err := requestParts(request, &ExternalRequestContext{}); err == nil {
		t.Fatal("partial external context succeeded")
	}
	for _, scheme := range []string{"ftp", "HTTP+unix"} {
		copy := request.Clone(request.Context())
		copy.URL.Scheme = scheme
		if _, err := requestParts(copy, nil); err == nil {
			t.Fatalf("scheme %q succeeded", scheme)
		}
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{}, RequestURI: "example.com:443", Host: "example.com:443"}
	if _, err := requestParts(connect, &ExternalRequestContext{Scheme: "https", Authority: "other.example:443", RequestTarget: "example.com:443"}); err == nil {
		t.Fatal("contradictory CONNECT external context succeeded")
	}
	relative := &http.Request{Method: http.MethodGet, URL: &url.URL{}, RequestURI: "relative", Host: "example.com"}
	if _, err := requestParts(relative, nil); err == nil {
		t.Fatal("relative request target succeeded")
	}
	tlsRequest := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/", Host: "example.com", TLS: &tls.ConnectionState{}}
	if parts, err := requestParts(tlsRequest, nil); err != nil || parts.scheme != "https" {
		t.Fatalf("TLS request scheme = %#v, %v", parts, err)
	}
	if externalForRequest(MessageContext{}, request) != nil {
		t.Fatal("externalForRequest returned unrelated context")
	}
	for _, authority := range []string{"", "user@example.com", "example.com:bad", "example.com:99999", "\u0080.example"} {
		if _, err := normalizeAuthority(authority, "https"); err == nil {
			t.Fatalf("normalizeAuthority(%q) succeeded", authority)
		}
	}
	if authority, err := normalizeAuthority("[2001:db8::1]:8443", "https"); err != nil || authority != "[2001:db8::1]:8443" {
		t.Fatalf("IPv6 authority = %q, %v", authority, err)
	}
}

func TestFieldAndStructuredSerializationBoundaries(t *testing.T) {
	t.Parallel()

	if value, err := normalizeFieldBytes(" a\r\n\t  b "); err != nil || value != "a b" {
		t.Fatalf("obs-fold normalization = %q, %v", value, err)
	}
	for _, value := range []string{"bad\rvalue", "bad\nvalue"} {
		if _, err := normalizeFieldBytes(value); err == nil {
			t.Fatalf("normalizeFieldBytes(%q) succeeded", value)
		}
	}
	if _, err := normalizeFieldValue("\u0080"); err == nil {
		t.Fatal("non-ASCII field value succeeded")
	}
	if _, err := normalizeFieldValue("bad\nvalue"); err == nil {
		t.Fatal("field normalization error was ignored")
	}
	for _, test := range []struct {
		values    []string
		fieldType StructuredFieldType
	}{
		{values: []string{"1", "2"}, fieldType: StructuredFieldItem},
		{values: []string{"Bad"}, fieldType: StructuredFieldDictionary},
		{values: []string{"1"}, fieldType: StructuredFieldType(255)},
	} {
		if _, err := strictStructuredField(test.values, test.fieldType); err == nil {
			t.Fatalf("strictStructuredField(%#v,%d) succeeded", test.values, test.fieldType)
		}
	}
	member, _ := httpsfv.UnmarshalItem([]string{"?1;a"})
	if value := marshalMember(member); value != "?1;a" {
		t.Fatalf("marshalMember(bare true) = %q", value)
	}
	if _, err := serializeComponentIdentifier(ComponentIdentifier{Name: "@method", Parameters: []Parameter{{Name: "UPPER", Value: true}}}); err == nil {
		t.Fatal("invalid component serialization succeeded")
	}
	if _, err := serializeSignatureParameters(SignatureInput{Components: []ComponentIdentifier{{Name: "@"}}}); err == nil {
		t.Fatal("invalid signature component serialization succeeded")
	}
}

func TestQueryAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		name string
	}{
		{raw: "", name: "x"},
		{raw: "%ZZ=1", name: "x"},
		{raw: "x=%ZZ", name: "x"},
		{raw: "x=1&x=2", name: "x"},
	} {
		if _, err := queryParameter(test.raw, test.name); err == nil {
			t.Fatalf("queryParameter(%q,%q) succeeded", test.raw, test.name)
		}
	}
	if value, err := queryParameter("x", "x"); err != nil || value != "" {
		t.Fatalf("valueless query = %q, %v", value, err)
	}
	if value, err := queryParameter("other=1&x=2", "x"); err != nil || value != "2" {
		t.Fatalf("query scan = %q, %v", value, err)
	}
	if got := formPercentEncode("a b+"); got != "a%20b%2B" {
		t.Fatalf("formPercentEncode() = %q", got)
	}
	if validFieldBaseValue("bad\x00") || asciiString("\u0080") {
		t.Fatal("prohibited field bytes accepted")
	}
	for _, parameters := range [][]Parameter{
		{{Name: "", Value: true}},
		{{Name: "UPPER", Value: true}},
		{{Name: "bad!", Value: true}},
		{{Name: "ok", Value: struct{}{}}},
	} {
		if validParametersForSerialization(parameters) {
			t.Fatalf("validParametersForSerialization(%#v) = true", parameters)
		}
	}
	if !strings.Contains("keep", "keep") {
		t.Fatal("unreachable")
	}
}

func TestMessageByteAndStatusExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, status := range []int{100, 999} {
		value, err := resolveDerived(MessageContext{Response: &http.Response{StatusCode: status}}, "@status", componentParameters{})
		if err != nil || value != strconv.Itoa(status) {
			t.Fatalf("status %d = %q, %v", status, value, err)
		}
	}
	for _, status := range []int{99, 1000} {
		if _, err := resolveDerived(MessageContext{Response: &http.Response{StatusCode: status}}, "@status", componentParameters{}); err == nil {
			t.Fatalf("status %d accepted", status)
		}
	}

	const allowed = "azAZ09*-._"
	if got := formPercentEncode(allowed); got != allowed {
		t.Fatalf("allowed form bytes = %q", got)
	}
	if got := formPercentEncode("`{: @"); got != "%60%7B%3A%20%40" {
		t.Fatalf("escaped form bytes = %q", got)
	}
	for _, value := range []string{"!", "~", "a b"} {
		if !validDerivedValue(value) {
			t.Fatalf("validDerivedValue(%q) = false", value)
		}
	}
	for _, value := range []string{"", " x", "x ", "\tx", "x\t", string([]byte{0x1f}), string([]byte{0x7f})} {
		if validDerivedValue(value) {
			t.Fatalf("validDerivedValue(%q) = true", value)
		}
	}
	for _, value := range []string{"\t", " ", "~"} {
		if !validFieldBaseValue(value) {
			t.Fatalf("validFieldBaseValue(%q) = false", value)
		}
	}
	for _, value := range []string{string([]byte{0x00}), string([]byte{0x1f}), string([]byte{0x7f}), string([]byte{0x80})} {
		if validFieldBaseValue(value) {
			t.Fatalf("validFieldBaseValue(%q) = true", value)
		}
	}
	if !asciiString(string([]byte{0x7f})) || asciiString(string([]byte{0x80})) {
		t.Fatal("ASCII boundary classified incorrectly")
	}
	for _, test := range []struct{ input, want string }{
		{"a\r\n b", "a b"}, {"a\r\n\t  b", "a b"}, {"a\r\n b\r\n\tc", "a b c"},
	} {
		if got, err := normalizeFieldBytes(test.input); err != nil || got != test.want {
			t.Fatalf("normalizeFieldBytes(%q) = %q, %v", test.input, got, err)
		}
	}
}

func TestExternalRequestSelectionBoundaries(t *testing.T) {
	t.Parallel()

	request := &http.Request{}
	related := &http.Request{}
	unrelated := &http.Request{}
	external := &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/"}
	context := MessageContext{Request: request, RelatedRequest: related, ExternalRequest: external}
	if externalForRequest(context, request) != external || externalForRequest(context, related) != external {
		t.Fatal("target request did not receive external context")
	}
	if externalForRequest(context, unrelated) != nil {
		t.Fatal("unrelated request received external context")
	}
}

func TestMatchingExternalAbsoluteAndAuthorityTargets(t *testing.T) {
	t.Parallel()

	absolute := &http.Request{
		Method: http.MethodGet, URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/a", RawQuery: "b=1"},
		RequestURI: "https://example.com/a?b=1", Host: "example.com",
	}
	parts, err := requestParts(absolute, &ExternalRequestContext{Scheme: "HTTPS", Authority: "EXAMPLE.COM:443", RequestTarget: absolute.RequestURI})
	if err != nil || parts.authority != "example.com" || parts.path != "/a" || parts.rawQuery != "b=1" {
		t.Fatalf("matching absolute target = %#v, %v", parts, err)
	}

	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{}, RequestURI: "example.com:443", Host: "example.com:443"}
	parts, err = requestParts(connect, &ExternalRequestContext{Scheme: "https", Authority: "EXAMPLE.COM:443", RequestTarget: connect.RequestURI})
	if err != nil || parts.authority != "example.com" || parts.requestTarget != "example.com:443" {
		t.Fatalf("matching authority target = %#v, %v", parts, err)
	}
}

func TestRequestPartsRejectsEachMalformedTargetBoundary(t *testing.T) {
	t.Parallel()

	base := func(method, target string) *http.Request {
		return &http.Request{Method: method, URL: &url.URL{Path: "/"}, RequestURI: target, Host: "example.com"}
	}
	tests := []struct {
		request  *http.Request
		external *ExternalRequestContext
	}{
		{base(http.MethodGet, "/"), &ExternalRequestContext{Scheme: "https", Authority: "example.com"}},
		{base(http.MethodGet, "http://[::1"), nil},
		{base(http.MethodGet, "http:///path"), nil},
		{base(http.MethodGet, "http://münich.example/"), &ExternalRequestContext{Scheme: "http", Authority: "münich.example", RequestTarget: "http://münich.example/"}},
		{base(http.MethodGet, "http://example.com:bad/"), &ExternalRequestContext{Scheme: "http", Authority: "example.com:bad", RequestTarget: "http://example.com:bad/"}},
		{base(http.MethodGet, "http://example.com/"), &ExternalRequestContext{Scheme: "http", Authority: "example.com:bad", RequestTarget: "http://example.com/"}},
		{base(http.MethodGet, "http://example.com/"), &ExternalRequestContext{Scheme: "http", Authority: "other.example", RequestTarget: "http://example.com/"}},
		{base(http.MethodGet, "/%zz"), nil},
		{base(http.MethodConnect, "[::1"), nil},
		{base(http.MethodConnect, "?"), nil},
		{base(http.MethodConnect, "münich.example:443"), &ExternalRequestContext{Scheme: "https", Authority: "münich.example:443", RequestTarget: "münich.example:443"}},
		{base(http.MethodConnect, "example.com:bad"), &ExternalRequestContext{Scheme: "https", Authority: "example.com:bad", RequestTarget: "example.com:bad"}},
		{base(http.MethodConnect, "example.com:443"), &ExternalRequestContext{Scheme: "https", Authority: "example.com:bad", RequestTarget: "example.com:443"}},
	}
	for _, test := range tests {
		if _, err := requestParts(test.request, test.external); err == nil {
			t.Fatalf("requestParts(%q, %#v) succeeded", test.request.RequestURI, test.external)
		}
	}
}
