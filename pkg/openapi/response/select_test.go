package response_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/response"
)

func TestSelectAppliesExactRangeAndDefaultPrecedence(t *testing.T) {
	t.Parallel()

	responses := responseObject(t,
		jsonvalue.Member{Name: "200", Value: responseString(t, "exact")},
		jsonvalue.Member{Name: "2XX", Value: responseString(t, "range")},
		jsonvalue.Member{Name: "default", Value: responseString(t, "default")},
	)
	for _, test := range []struct {
		status int
		key    string
		kind   response.MatchKind
		value  string
	}{
		{status: 200, key: "200", kind: response.MatchExact, value: "exact"},
		{status: 201, key: "2XX", kind: response.MatchRange, value: "range"},
		{status: 404, key: "default", kind: response.MatchDefault, value: "default"},
	} {
		match, found, err := response.Select(responses, test.status)
		if err != nil {
			t.Fatal(err)
		}
		if !found || match.Key != test.key || match.Kind != test.kind {
			t.Fatalf("Select(%d) = %#v, %t", test.status, match, found)
		}
		value, _ := match.Value.Text()
		if value != test.value {
			t.Fatalf("Select(%d) value = %q", test.status, value)
		}
	}
}

func TestSelectHandlesMissingAndInvalidInputs(t *testing.T) {
	t.Parallel()

	empty := responseObject(t)
	if _, found, err := response.Select(empty, 204); err != nil || found {
		t.Fatalf("empty Select() found = %t, error = %v", found, err)
	}
	if _, _, err := response.Select(jsonvalue.Null(), 200); !errors.Is(
		err, response.ErrInvalidResponses,
	) {
		t.Fatalf("non-object error = %v", err)
	}
	for _, status := range []int{99, 600} {
		if _, _, err := response.Select(empty, status); !errors.Is(
			err, response.ErrInvalidStatus,
		) {
			t.Fatalf("status %d error = %v", status, err)
		}
	}
}

func TestHeadersIgnoresContentTypeCaseInsensitively(t *testing.T) {
	t.Parallel()

	value := responseObject(t,
		jsonvalue.Member{Name: "description", Value: responseString(t, "ok")},
		jsonvalue.Member{Name: "headers", Value: responseObject(t,
			jsonvalue.Member{Name: "X-Trace", Value: responseObject(t)},
			jsonvalue.Member{Name: "content-type", Value: responseObject(t)},
			jsonvalue.Member{Name: "X-Limit", Value: responseObject(t)},
		)},
	)
	headers, err := response.Headers(value, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 || headers[0].Name != "X-Trace" ||
		headers[1].Name != "X-Limit" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestHeadersValidatesValuesAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		response jsonvalue.Value
		maximum  int
		want     error
	}{
		{response: jsonvalue.Null(), maximum: 1,
			want: response.ErrInvalidResponseHeaders},
		{response: responseObject(t), maximum: 0,
			want: response.ErrInvalidResponseHeaders},
		{response: responseObject(t,
			jsonvalue.Member{Name: "headers", Value: jsonvalue.Null()}),
			maximum: 1, want: response.ErrInvalidResponseHeaders},
		{response: responseObject(t,
			jsonvalue.Member{Name: "headers", Value: responseObject(t,
				jsonvalue.Member{Name: "X", Value: jsonvalue.Null()},
			)}), maximum: 1, want: response.ErrInvalidResponseHeaders},
		{response: responseObject(t,
			jsonvalue.Member{Name: "headers", Value: responseObject(t,
				jsonvalue.Member{Name: "X", Value: responseObject(t)},
				jsonvalue.Member{Name: "Y", Value: responseObject(t)},
			)}), maximum: 1, want: response.ErrResponseHeaderLimit},
	} {
		_, err := response.Headers(test.response, test.maximum)
		if !errors.Is(err, test.want) {
			t.Fatalf("Headers() error = %v, want %v", err, test.want)
		}
	}
	headers, err := response.Headers(responseObject(t), 1)
	if err != nil || len(headers) != 0 {
		t.Fatalf("absent headers = %#v, %v", headers, err)
	}
}

func TestSetCookieValuesPreservesPreEncodedSeparateValues(t *testing.T) {
	t.Parallel()

	input := []string{
		"session=a%2Fb; Path=/; HttpOnly",
		"token=YWJjZA==; Secure",
	}
	values, err := response.SetCookieValues(input, 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != input[0] || values[1] != input[1] {
		t.Fatalf("values = %#v", values)
	}
	input[0] = "changed"
	if values[0] == input[0] {
		t.Fatal("SetCookieValues retained caller storage")
	}
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), "set-cookie:") {
			t.Fatalf("field name included in value %q", value)
		}
	}
}

func TestSetCookieValuesValidatesLinesAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		values    []string
		maxValues int
		maxBytes  int
		want      error
	}{
		{maxValues: 0, maxBytes: 1, want: response.ErrInvalidSetCookieValues},
		{maxValues: 1, maxBytes: 0, want: response.ErrInvalidSetCookieValues},
		{values: []string{"a=1", "b=2"}, maxValues: 1, maxBytes: 10,
			want: response.ErrSetCookieLimit},
		{values: []string{"long=value"}, maxValues: 1, maxBytes: 4,
			want: response.ErrSetCookieLimit},
		{values: []string{"a=1\r\nX: injected"}, maxValues: 1, maxBytes: 100,
			want: response.ErrInvalidSetCookieValues},
		{values: []string{string([]byte{0xff})}, maxValues: 1, maxBytes: 100,
			want: response.ErrInvalidSetCookieValues},
	} {
		_, err := response.SetCookieValues(
			test.values, test.maxValues, test.maxBytes,
		)
		if !errors.Is(err, test.want) {
			t.Fatalf("SetCookieValues() error = %v, want %v", err, test.want)
		}
	}
}

func TestLinksetHeaderValueReplacesDocumentNewlines(t *testing.T) {
	t.Parallel()

	value, err := response.LinksetHeaderValue(
		[]byte("<https://example.test/next>; rel=\"next\",\r\n"+
			"\t<https://example.test/prev>; rel=\"prev\"\n"),
		1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "<https://example.test/next>; rel=\"next\", " +
		"\t<https://example.test/prev>; rel=\"prev\" "
	if value != want {
		t.Fatalf("LinksetHeaderValue() = %q, want %q", value, want)
	}
	if strings.ContainsAny(value, "\r\n") {
		t.Fatalf("header value retains a newline: %q", value)
	}
}

func TestLinksetHeaderValueValidatesFieldSyntaxAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		document []byte
		maximum  int
		want     error
	}{
		{maximum: 0, want: response.ErrInvalidLinksetHeader},
		{document: []byte("<a>; rel=next"), maximum: 3,
			want: response.ErrLinksetHeaderLimit},
		{document: []byte("<https://example.test/\xc3\xa9>; rel=next"),
			maximum: 100, want: response.ErrInvalidLinksetHeader},
		{document: []byte("<a>; rel=next\x00"), maximum: 100,
			want: response.ErrInvalidLinksetHeader},
		{document: []byte("<a>; rel=next\x7f"), maximum: 100,
			want: response.ErrInvalidLinksetHeader},
	} {
		_, err := response.LinksetHeaderValue(test.document, test.maximum)
		if !errors.Is(err, test.want) {
			t.Fatalf("LinksetHeaderValue() error = %v, want %v", err, test.want)
		}
	}
}

func TestResponseHelpersAcceptEveryExactBoundary(t *testing.T) {
	t.Parallel()

	empty := responseObject(t)
	for _, status := range []int{100, 599} {
		if _, found, err := response.Select(empty, status); err != nil || found {
			t.Fatalf("Select(%d) = found %t, %v", status, found, err)
		}
	}
	values, err := response.SetCookieValues([]string{"a", "b"}, 2, 2)
	if err != nil || len(values) != 2 {
		t.Fatalf("exact Set-Cookie limits = %#v, %v", values, err)
	}
	if values, err := response.SetCookieValues(nil, 1, 1); err != nil || len(values) != 0 {
		t.Fatalf("minimum Set-Cookie limits = %#v, %v", values, err)
	}
	if _, err := response.SetCookieValues([]string{"aa", "aa"}, 2, 3); !errors.Is(
		err, response.ErrSetCookieLimit,
	) {
		t.Fatalf("cumulative Set-Cookie limit error = %v", err)
	}
	for _, document := range [][]byte{
		{' '},
		{'~'},
		{'\t'},
		{'\r'},
		{'\n'},
		{'\r', '\n'},
	} {
		value, err := response.LinksetHeaderValue(document, len(document))
		if err != nil {
			t.Fatalf("exact linkset limit for %q: %v", document, err)
		}
		if strings.ContainsAny(value, "\r\n") {
			t.Fatalf("linkset %q retains newline as %q", document, value)
		}
	}
}

func responseObject(t *testing.T, members ...jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func responseString(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
