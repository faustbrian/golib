package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestNewRequestSpecResolvesSameOriginReference(t *testing.T) {
	t.Parallel()

	spec, err := NewRequestSpec(
		"https://api.example.com/v1/",
		"widgets/a%2Fb?include=owner#details",
	)
	if err != nil {
		t.Fatalf("NewRequestSpec() error = %v", err)
	}

	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if request.URL.String() != "https://api.example.com/v1/widgets/a%2Fb?include=owner#details" {
		t.Fatalf("URL = %q", request.URL.String())
	}
	if request.URL.Path != "/v1/widgets/a/b" {
		t.Fatalf("URL.Path = %q, want decoded path", request.URL.Path)
	}
	if request.URL.EscapedPath() != "/v1/widgets/a%2Fb" {
		t.Fatalf("URL.EscapedPath() = %q, want encoded slash preserved", request.URL.EscapedPath())
	}
	if request.RequestURI != "" {
		t.Fatalf("RequestURI = %q, want empty for client request", request.RequestURI)
	}
}

func TestNewRequestSpecRejectsUnsafeBaseOrReference(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		base      string
		reference string
	}{
		"relative base": {
			base:      "/v1/",
			reference: "widgets",
		},
		"unsupported scheme": {
			base:      "ftp://api.example.com/v1/",
			reference: "widgets",
		},
		"missing host": {
			base:      "https:///v1/",
			reference: "widgets",
		},
		"base user information": {
			base:      "https://user:secret@api.example.com/v1/",
			reference: "widgets",
		},
		"absolute reference": {
			base:      "https://api.example.com/v1/",
			reference: "https://evil.example/widgets",
		},
		"network path reference": {
			base:      "https://api.example.com/v1/",
			reference: "//evil.example/widgets",
		},
		"reference user information": {
			base:      "https://api.example.com/v1/",
			reference: "//user:secret@api.example.com/widgets",
		},
		"malformed reference escape": {
			base:      "https://api.example.com/v1/",
			reference: "widgets/%zz",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRequestSpec(test.base, test.reference)
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("NewRequestSpec() error = %v, want ErrInvalidURL", err)
			}
		})
	}
}

func TestRequestSpecAppliesDeterministicLayerPrecedence(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/v1/", "widgets?source=reference")
	var err error

	layers := []RequestLayer{
		LayerClient,
		LayerEndpoint,
		LayerRequest,
		LayerAuthentication,
		LayerSigning,
		LayerOneShot,
	}
	for _, layer := range layers {
		value := layer.String()
		spec, err = spec.WithHeader(layer, "X-Policy", value)
		if err != nil {
			t.Fatalf("WithHeader(%s) error = %v", layer, err)
		}
		spec, err = spec.WithQuery(layer, "source", RepeatedQuery(value))
		if err != nil {
			t.Fatalf("WithQuery(%s) error = %v", layer, err)
		}
	}

	spec, err = spec.WithHeader(LayerRequest, "X-Multi", "first")
	if err != nil {
		t.Fatalf("WithHeader() error = %v", err)
	}
	spec, err = spec.AddHeader(LayerRequest, "X-Multi", "second", "third")
	if err != nil {
		t.Fatalf("AddHeader() error = %v", err)
	}
	spec, err = spec.WithQuery(LayerRequest, "tag", RepeatedQuery("a", "b"))
	if err != nil {
		t.Fatalf("WithQuery() error = %v", err)
	}

	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if request.Header.Get("X-Policy") != LayerOneShot.String() {
		t.Fatalf("X-Policy = %q, want one-shot value", request.Header.Get("X-Policy"))
	}
	if got := request.Header.Values("X-Multi"); !reflect.DeepEqual(got, []string{"first", "second", "third"}) {
		t.Fatalf("X-Multi = %#v", got)
	}
	if request.URL.Query().Get("source") != LayerOneShot.String() {
		t.Fatalf("source = %q, want one-shot value", request.URL.Query().Get("source"))
	}
	if got := request.URL.Query()["tag"]; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("tag = %#v", got)
	}
	if request.URL.RawQuery != "source=one-shot&tag=a&tag=b" {
		t.Fatalf("RawQuery = %q, want canonical ordering", request.URL.RawQuery)
	}
}

func TestRequestSpecCanRemoveInheritedHeadersAndQuery(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/v1/", "widgets?keep=yes&remove=reference")
	var err error
	spec, err = spec.WithHeader(LayerClient, "X-Remove", "client")
	if err != nil {
		t.Fatalf("WithHeader() error = %v", err)
	}
	spec, err = spec.WithQuery(LayerClient, "remove-client", RepeatedQuery("client"))
	if err != nil {
		t.Fatalf("WithQuery() error = %v", err)
	}
	spec, err = spec.WithoutHeader(LayerRequest, "X-Remove")
	if err != nil {
		t.Fatalf("WithoutHeader() error = %v", err)
	}
	spec, err = spec.WithoutQuery(LayerRequest, "remove")
	if err != nil {
		t.Fatalf("WithoutQuery() error = %v", err)
	}
	spec, err = spec.WithoutQuery(LayerRequest, "remove-client")
	if err != nil {
		t.Fatalf("WithoutQuery() error = %v", err)
	}

	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if request.Header.Get("X-Remove") != "" {
		t.Fatalf("X-Remove = %q, want absent", request.Header.Get("X-Remove"))
	}
	if request.URL.RawQuery != "keep=yes" {
		t.Fatalf("RawQuery = %q, want keep=yes", request.URL.RawQuery)
	}
}

func TestRequestSpecAndBuiltRequestsDoNotAliasMutableState(t *testing.T) {
	t.Parallel()

	original := mustRequestSpec(t, "https://api.example.com/v1/", "widgets")
	derived, err := original.WithHeader(LayerRequest, "X-Test", "derived")
	if err != nil {
		t.Fatalf("WithHeader() error = %v", err)
	}
	derived, err = derived.WithQuery(LayerRequest, "filter", RepeatedQuery("derived"))
	if err != nil {
		t.Fatalf("WithQuery() error = %v", err)
	}

	originalRequest, err := original.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("original Build() error = %v", err)
	}
	first, err := derived.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	second, err := derived.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}

	first.Header.Set("X-Test", "mutated")
	first.URL.Path = "/mutated"
	first.URL.RawQuery = "filter=mutated"

	if originalRequest.Header.Get("X-Test") != "" || originalRequest.URL.RawQuery != "" {
		t.Fatalf("original request inherited derived state: %#v %q", originalRequest.Header, originalRequest.URL.RawQuery)
	}
	if second.Header.Get("X-Test") != "derived" {
		t.Fatalf("second X-Test = %q, want derived", second.Header.Get("X-Test"))
	}
	if second.URL.Path != "/v1/widgets" {
		t.Fatalf("second URL.Path = %q", second.URL.Path)
	}
	if second.URL.RawQuery != "filter=derived" {
		t.Fatalf("second RawQuery = %q", second.URL.RawQuery)
	}
}

func TestRequestSpecRejectsInvalidLayerHeaderAndQuery(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")

	if _, err := spec.WithHeader(RequestLayer(255), "X-Test", "value"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("WithHeader() error = %v, want ErrInvalidRequestSpec", err)
	}
	if _, err := spec.WithHeader(LayerRequest, "Bad Header", "value"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("WithHeader() error = %v, want ErrInvalidHeader", err)
	}
	if _, err := spec.WithHeader(LayerRequest, "X-Test", "safe\r\ninjected: true"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("WithHeader() error = %v, want ErrInvalidHeader", err)
	}
	if _, err := spec.AddHeader(LayerRequest, "", "value"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("AddHeader() error = %v, want ErrInvalidHeader", err)
	}
	if _, err := spec.WithQuery(LayerRequest, "", RepeatedQuery("value")); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("WithQuery() error = %v, want ErrInvalidQuery", err)
	}
	if _, err := spec.WithoutQuery(RequestLayer(255), "name"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("WithoutQuery() error = %v, want ErrInvalidRequestSpec", err)
	}
}

func TestRequestSpecSerializesQueryStrategiesAndValueStates(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	comma, err := QueryValues(QueryCommaDelimited, "a", "b")
	if err != nil {
		t.Fatalf("QueryValues(comma) error = %v", err)
	}
	space, err := QueryValues(QuerySpaceDelimited, "a", "b")
	if err != nil {
		t.Fatalf("QueryValues(space) error = %v", err)
	}
	pipe, err := QueryValues(QueryPipeDelimited, "a", "b")
	if err != nil {
		t.Fatalf("QueryValues(pipe) error = %v", err)
	}
	custom, err := CustomQuery(QueryEncoderFunc(func(name string) ([]QueryPart, error) {
		return []QueryPart{
			{Name: name + "[first]", Value: "one", HasValue: true},
			{Name: name + "[present]"},
		}, nil
	}))
	if err != nil {
		t.Fatalf("CustomQuery() error = %v", err)
	}

	parameters := []struct {
		name  string
		value QueryValue
	}{
		{name: "repeated", value: RepeatedQuery("a", "b")},
		{name: "comma", value: comma},
		{name: "space", value: space},
		{name: "pipe", value: pipe},
		{name: "deep", value: DeepObjectQuery(map[string]string{"z": "last", "a": "first"})},
		{name: "null", value: NullQuery()},
		{name: "empty", value: RepeatedQuery("")},
		{name: "zero", value: RepeatedQuery("0")},
		{name: "omitted-empty-array", value: RepeatedQuery()},
		{name: "custom", value: custom},
	}
	for _, parameter := range parameters {
		spec, err = spec.WithQuery(LayerRequest, parameter.name, parameter.value)
		if err != nil {
			t.Fatalf("WithQuery(%s) error = %v", parameter.name, err)
		}
	}

	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := "comma=a%2Cb" +
		"&custom%5Bfirst%5D=one" +
		"&custom%5Bpresent%5D" +
		"&deep%5Ba%5D=first" +
		"&deep%5Bz%5D=last" +
		"&empty=" +
		"&null" +
		"&pipe=a%7Cb" +
		"&repeated=a&repeated=b" +
		"&space=a%20b" +
		"&zero=0"
	if request.URL.RawQuery != want {
		t.Fatalf("RawQuery = %q, want %q", request.URL.RawQuery, want)
	}
}

func TestDeepObjectQueryCopiesCallerMap(t *testing.T) {
	t.Parallel()

	fields := map[string]string{"role": "admin"}
	value := DeepObjectQuery(fields)
	fields["role"] = "mutated"
	fields["extra"] = "mutated"

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err := spec.WithQuery(LayerRequest, "filter", value)
	if err != nil {
		t.Fatalf("WithQuery() error = %v", err)
	}
	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if request.URL.RawQuery != "filter%5Brole%5D=admin" {
		t.Fatalf("RawQuery = %q", request.URL.RawQuery)
	}
}

func TestRequestSpecReportsCustomQueryEncoderFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("encoder failed")
	value, err := CustomQuery(QueryEncoderFunc(func(string) ([]QueryPart, error) {
		return nil, wantErr
	}))
	if err != nil {
		t.Fatalf("CustomQuery() error = %v", err)
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithQuery(LayerRequest, "filter", value)
	if err != nil {
		t.Fatalf("WithQuery() error = %v", err)
	}

	_, err = spec.Build(context.Background(), http.MethodGet)
	var encodingErr *QueryEncodingError
	if !errors.As(err, &encodingErr) {
		t.Fatalf("Build() error = %T %v, want *QueryEncodingError", err, err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Build() error = %v, want cause %v", err, wantErr)
	}
	if encodingErr.Parameter != "filter" {
		t.Fatalf("QueryEncodingError.Parameter = %q", encodingErr.Parameter)
	}
}

func TestRequestSpecRejectsInvalidQueryStrategiesAndOutput(t *testing.T) {
	t.Parallel()

	if _, err := QueryValues(QueryRepeated, "value"); err != nil {
		t.Fatalf("QueryValues(repeated) error = %v", err)
	}
	if _, err := QueryValues(QueryStyle(255), "value"); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("QueryValues(unknown) error = %v, want ErrInvalidQuery", err)
	}
	if _, err := CustomQuery(nil); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("CustomQuery(nil) error = %v, want ErrInvalidQuery", err)
	}

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	if _, err := spec.WithQuery(LayerRequest, "invalid", RepeatedQuery(string([]byte{0xff}))); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("WithQuery(invalid UTF-8) error = %v, want ErrInvalidQuery", err)
	}

	invalidOutput, err := CustomQuery(QueryEncoderFunc(func(string) ([]QueryPart, error) {
		return []QueryPart{{Name: "", Value: "value", HasValue: true}}, nil
	}))
	if err != nil {
		t.Fatalf("CustomQuery() error = %v", err)
	}
	spec, err = spec.WithQuery(LayerRequest, "custom", invalidOutput)
	if err != nil {
		t.Fatalf("WithQuery(custom) error = %v", err)
	}
	if _, err := spec.Build(context.Background(), http.MethodGet); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Build() error = %v, want ErrInvalidQuery", err)
	}
}

func TestRequestSpecRejectsMalformedReferenceQueryAtBuild(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets?first=1;second=2")
	if _, err := spec.Build(context.Background(), http.MethodGet); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Build() error = %v, want ErrInvalidQuery", err)
	}
}

func TestRequestSpecBuildsIndependentReplayableByteBodies(t *testing.T) {
	t.Parallel()

	content := []byte(`{"name":"original"}`)
	body, err := NewBytesBody("application/json", content)
	if err != nil {
		t.Fatalf("NewBytesBody() error = %v", err)
	}
	copy(content, []byte(`{"name":"mutated!"}`))

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}

	first, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	t.Cleanup(func() { _ = first.Body.Close() })
	second, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	t.Cleanup(func() { _ = second.Body.Close() })

	want := `{"name":"original"}`
	if got := readBody(t, first.Body); got != want {
		t.Fatalf("first body = %q, want %q", got, want)
	}
	if got := readBody(t, second.Body); got != want {
		t.Fatalf("second body = %q, want %q", got, want)
	}
	if first.ContentLength != int64(len(want)) {
		t.Fatalf("ContentLength = %d, want %d", first.ContentLength, len(want))
	}
	if first.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", first.Header.Get("Content-Type"))
	}
	if first.GetBody == nil {
		t.Fatal("GetBody is nil for replayable body")
	}
	replay, err := first.GetBody()
	if err != nil {
		t.Fatalf("GetBody() error = %v", err)
	}
	defer func() {
		if err := replay.Close(); err != nil {
			t.Errorf("replay Close() error = %v", err)
		}
	}()
	if got := readBody(t, replay); got != want {
		t.Fatalf("replayed body = %q, want %q", got, want)
	}
}

func TestRequestSpecExplicitContentTypeOverridesBodyDefault(t *testing.T) {
	t.Parallel()

	body, err := NewBytesBody("application/json", []byte("{}"))
	if err != nil {
		t.Fatalf("NewBytesBody() error = %v", err)
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}
	spec, err = spec.WithHeader(LayerRequest, "Content-Type", "application/problem+json")
	if err != nil {
		t.Fatalf("WithHeader() error = %v", err)
	}

	request, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer func() {
		if err := request.Body.Close(); err != nil {
			t.Errorf("Body.Close() error = %v", err)
		}
	}()
	if request.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
	}
}

func TestRequestSpecStreamingBodyIsExplicitlyOneShot(t *testing.T) {
	t.Parallel()

	reader := &observableReadCloser{Reader: strings.NewReader("stream")}
	body, err := NewStreamingBody("text/plain", 6, reader)
	if err != nil {
		t.Fatalf("NewStreamingBody() error = %v", err)
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}

	request, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	if request.GetBody != nil {
		t.Fatal("GetBody is non-nil for streaming body")
	}
	if got := readBody(t, request.Body); got != "stream" {
		t.Fatalf("body = %q, want stream", got)
	}
	if err := request.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
	if !reader.closed {
		t.Fatal("closing request body did not close caller-provided stream")
	}

	_, err = spec.Build(context.Background(), http.MethodPost)
	if !errors.Is(err, ErrBodyConsumed) {
		t.Fatalf("second Build() error = %v, want ErrBodyConsumed", err)
	}
}

func TestRequestSpecReportsReplayableBodyOpenFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("open failed")
	body, err := NewReplayableBody("application/octet-stream", 10, BodyOpener(func() (io.ReadCloser, error) {
		return nil, wantErr
	}))
	if err != nil {
		t.Fatalf("NewReplayableBody() error = %v", err)
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}

	_, err = spec.Build(context.Background(), http.MethodPost)
	var openErr *BodyOpenError
	if !errors.As(err, &openErr) {
		t.Fatalf("Build() error = %T %v, want *BodyOpenError", err, err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Build() error = %v, want cause %v", err, wantErr)
	}
}

func TestRequestSpecRejectsInvalidBodyPolicies(t *testing.T) {
	t.Parallel()

	if _, err := NewBytesBody("text/plain\r\ninjected: true", nil); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewBytesBody(invalid media type) error = %v, want ErrInvalidBody", err)
	}
	if _, err := NewReplayableBody("text/plain", -2, BodyOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	})); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewReplayableBody(invalid length) error = %v, want ErrInvalidBody", err)
	}
	if _, err := NewReplayableBody("text/plain", 0, nil); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewReplayableBody(nil opener) error = %v, want ErrInvalidBody", err)
	}
	if _, err := NewStreamingBody("text/plain", 0, nil); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewStreamingBody(nil reader) error = %v, want ErrInvalidBody", err)
	}
	if _, err := NewStreamingBody("text/plain", -2, io.NopCloser(bytes.NewReader(nil))); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewStreamingBody(invalid length) error = %v, want ErrInvalidBody", err)
	}

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	if _, err := spec.WithBody(nil); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("WithBody(nil) error = %v, want ErrInvalidBody", err)
	}
	if _, err := spec.WithBody(&mutableRequestBody{contentLength: -2}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("WithBody(invalid metadata) error = %v, want ErrInvalidBody", err)
	}
	spec, err := spec.WithBody(nilOpeningBody{})
	if err != nil {
		t.Fatalf("WithBody(nilOpeningBody) error = %v", err)
	}
	if _, err := spec.Build(context.Background(), http.MethodPost); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("Build() error = %v, want ErrInvalidBody", err)
	}
}

func TestRequestSpecRejectsTypedNilExtensions(t *testing.T) {
	t.Parallel()

	var body *mutableRequestBody
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	if _, err := spec.WithBody(body); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("WithBody(typed nil) error = %v, want ErrInvalidBody", err)
	}

	var encoder *nilQueryEncoder
	if _, err := CustomQuery(encoder); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("CustomQuery(typed nil) error = %v, want ErrInvalidQuery", err)
	}

	var reader *observableReadCloser
	if _, err := NewStreamingBody("text/plain", 0, reader); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("NewStreamingBody(typed nil) error = %v, want ErrInvalidBody", err)
	}
}

func TestRequestSpecClosesBodyWhenRequestConstructionFails(t *testing.T) {
	t.Parallel()

	reader := &observableReadCloser{Reader: strings.NewReader("stream")}
	body, err := NewStreamingBody("", -1, reader)
	if err != nil {
		t.Fatalf("NewStreamingBody() error = %v", err)
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err = spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}

	if _, err := spec.Build(context.Background(), "bad method"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("Build(invalid method) error = %v, want ErrInvalidRequestSpec", err)
	}
	if !reader.closed {
		t.Fatal("failed request construction did not close opened stream")
	}
}

func TestRequestSpecBoundaryContracts(t *testing.T) {
	t.Parallel()

	if RequestLayer(255).String() != "layer(255)" {
		t.Fatalf("invalid layer string = %q", RequestLayer(255).String())
	}
	if _, err := (RequestSpec{}).Build(context.Background(), http.MethodGet); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("zero Build() error = %v, want ErrInvalidRequestSpec", err)
	}

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	if _, err := spec.AddHeader(RequestLayer(255), "X-Test", "value"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("AddHeader(invalid layer) error = %v", err)
	}
	if _, err := spec.WithoutHeader(RequestLayer(255), "X-Test"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("WithoutHeader(invalid layer) error = %v", err)
	}
	if _, err := spec.WithoutHeader(LayerRequest, "Bad Header"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("WithoutHeader(invalid name) error = %v", err)
	}
	if _, err := spec.WithHeader(LayerRequest, "X-Empty"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("WithHeader(no values) error = %v", err)
	}
	if _, err := spec.WithQuery(RequestLayer(255), "name", RepeatedQuery("value")); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("WithQuery(invalid layer) error = %v", err)
	}
	if _, err := spec.WithoutQuery(LayerRequest, ""); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("WithoutQuery(invalid name) error = %v", err)
	}

	spec, err := spec.WithoutHeader(LayerRequest, "X-Restored")
	if err != nil {
		t.Fatalf("WithoutHeader() error = %v", err)
	}
	spec, err = spec.AddHeader(LayerRequest, "X-Restored", "value\twith-tab")
	if err != nil {
		t.Fatalf("AddHeader(after removal) error = %v", err)
	}
	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if request.Header.Get("X-Restored") != "value\twith-tab" {
		t.Fatalf("X-Restored = %q", request.Header.Get("X-Restored"))
	}

	body, err := NewBytesBody("text/plain", []byte("body"))
	if err != nil {
		t.Fatalf("NewBytesBody() error = %v", err)
	}
	withBody, err := spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}
	withoutBody := withBody.WithoutBody()
	request, err = withoutBody.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("Build(without body) error = %v", err)
	}
	if request.Body != nil {
		t.Fatalf("Body = %T, want nil", request.Body)
	}
}

func TestQueryBoundaryContracts(t *testing.T) {
	t.Parallel()

	if _, err := QueryValues(QueryCommaDelimited, string([]byte{0xff})); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("QueryValues(invalid UTF-8) error = %v", err)
	}
	var function QueryEncoderFunc
	if _, err := CustomQuery(function); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("CustomQuery(typed nil function) error = %v", err)
	}

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets?=invalid")
	if _, err := spec.Build(context.Background(), http.MethodGet); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Build(empty reference name) error = %v", err)
	}

	invalidValues := []QueryValue{
		{},
		{style: QueryStyle(255), valid: true},
		DeepObjectQuery(map[string]string{"": "value"}),
		DeepObjectQuery(map[string]string{"name": string([]byte{0xff})}),
		{style: queryCustom, valid: true},
	}
	for index, value := range invalidValues {
		if _, err := spec.WithQuery(LayerRequest, "value", value); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("WithQuery(invalid %d) error = %v", index, err)
		}
	}

	emptyDelimited, err := QueryValues(QueryCommaDelimited)
	if err != nil {
		t.Fatalf("QueryValues(empty) error = %v", err)
	}
	clean := mustRequestSpec(t, "https://api.example.com/", "widgets")
	clean, err = clean.WithQuery(LayerRequest, "empty", emptyDelimited)
	if err != nil {
		t.Fatalf("WithQuery(empty) error = %v", err)
	}
	request, err := clean.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatalf("Build(empty) error = %v", err)
	}
	if request.URL.RawQuery != "" {
		t.Fatalf("RawQuery = %q, want empty", request.URL.RawQuery)
	}

	invalidCustom, err := CustomQuery(QueryEncoderFunc(func(string) ([]QueryPart, error) {
		return []QueryPart{{Name: "value", Value: string([]byte{0xff}), HasValue: true}}, nil
	}))
	if err != nil {
		t.Fatalf("CustomQuery() error = %v", err)
	}
	clean, err = clean.WithQuery(LayerRequest, "custom", invalidCustom)
	if err != nil {
		t.Fatalf("WithQuery(custom) error = %v", err)
	}
	if _, err := clean.Build(context.Background(), http.MethodGet); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Build(invalid custom value) error = %v", err)
	}

	if _, err := encodeQuery(map[string]QueryValue{"invalid": {}}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("encodeQuery(invalid value) error = %v", err)
	}
	if _, err := encodeQueryValue("invalid", QueryValue{style: QueryStyle(255), valid: true}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("encodeQueryValue(invalid style) error = %v", err)
	}
}

func TestRequestSpecRejectsBodyMetadataChangedAfterAttachment(t *testing.T) {
	t.Parallel()

	body := &mutableRequestBody{
		contentType:   "text/plain",
		contentLength: 4,
		opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("body")), nil
		},
	}
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, err := spec.WithBody(body)
	if err != nil {
		t.Fatalf("WithBody() error = %v", err)
	}
	body.contentLength = -2

	if _, err := spec.Build(context.Background(), http.MethodPost); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("Build() error = %v, want ErrInvalidBody", err)
	}
}

func TestErrorMessagesDoNotRenderSensitiveCauses(t *testing.T) {
	t.Parallel()

	secret := errors.New("secret payload")
	bodyErr := &BodyOpenError{Cause: secret}
	if strings.Contains(bodyErr.Error(), secret.Error()) {
		t.Fatalf("BodyOpenError = %q contains cause", bodyErr.Error())
	}
	queryErr := &QueryEncodingError{Parameter: "filter", Cause: secret}
	if strings.Contains(queryErr.Error(), secret.Error()) {
		t.Fatalf("QueryEncodingError = %q contains cause", queryErr.Error())
	}
}

func TestCloneURLCopiesUserInformation(t *testing.T) {
	t.Parallel()

	source, err := url.Parse("https://user:secret@example.com/path")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	clone := cloneURL(source)
	clone.User = url.UserPassword("other", "changed")
	if source.User.String() != "user:secret" {
		t.Fatalf("source user information = %q", source.User.String())
	}
	if cloneURL(nil) != nil {
		t.Fatal("cloneURL(nil) is non-nil")
	}
	if sameOrigin(source, &url.URL{Scheme: "https", Host: "other.example"}) {
		t.Fatal("sameOrigin accepted different hosts")
	}
}

func readBody(t *testing.T, reader io.Reader) string {
	t.Helper()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	return string(content)
}

type observableReadCloser struct {
	io.Reader
	closed bool
}

func (reader *observableReadCloser) Close() error {
	reader.closed = true

	return nil
}

type nilOpeningBody struct{}

func (nilOpeningBody) Open() (io.ReadCloser, error) { return nil, nil }
func (nilOpeningBody) Replayable() bool             { return true }
func (nilOpeningBody) ContentLength() int64         { return 0 }
func (nilOpeningBody) ContentType() string          { return "text/plain" }

type mutableRequestBody struct {
	contentType   string
	contentLength int64
	opener        BodyOpener
}

func (body *mutableRequestBody) Open() (io.ReadCloser, error) { return body.opener() }
func (*mutableRequestBody) Replayable() bool                  { return true }
func (body *mutableRequestBody) ContentLength() int64         { return body.contentLength }
func (body *mutableRequestBody) ContentType() string          { return body.contentType }

type nilQueryEncoder struct{}

func (*nilQueryEncoder) EncodeQuery(string) ([]QueryPart, error) { return nil, nil }

func mustRequestSpec(t *testing.T, baseURL string, reference string) RequestSpec {
	t.Helper()

	spec, err := NewRequestSpec(baseURL, reference)
	if err != nil {
		t.Fatalf("NewRequestSpec() error = %v", err)
	}

	return spec
}
