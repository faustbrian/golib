package httpsignature

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestHardeningFuzzOraclesDetectMutations(t *testing.T) {
	t.Parallel()

	accept, err := ParseAcceptSignatures([]string{`sig=("@method");created;keyid="key"`})
	if err != nil {
		t.Fatalf("ParseAcceptSignatures() error = %v", err)
	}
	mutatedAccept := AcceptSignatures{entries: accept.Entries()}
	mutatedAccept.entries[0].Label = "other"
	if err := equivalentAcceptSignatures(accept, mutatedAccept); err == nil {
		t.Fatal("Accept-Signature oracle accepted a mutated label")
	}

	preferences, err := ParseDigestPreferences([]string{"sha-256=10"})
	if err != nil {
		t.Fatalf("ParseDigestPreferences() error = %v", err)
	}
	mutatedPreferences := DigestPreferences{entries: preferences.Entries()}
	mutatedPreferences.entries[0].Weight = 0
	if err := equivalentDigestPreferences(preferences, mutatedPreferences); err == nil {
		t.Fatal("digest-preference oracle accepted a mutated weight")
	}

	if err := equivalentSignatureBaseResults("base one", nil, "base two", nil); err == nil {
		t.Fatal("signature-base oracle accepted divergent canonical bases")
	}
}

func TestHardeningFuzzValidSeedsReachOwnedContracts(t *testing.T) {
	t.Parallel()

	accept, err := ParseAcceptSignatures([]string{
		`one=("@method");created`,
		`two=("@status");expires;tag="response"`,
	})
	if err != nil || len(accept.Entries()) != 2 {
		t.Fatalf("multiline Accept-Signature seed = %#v, %v", accept.Entries(), err)
	}
	preferences, err := ParseDigestPreferences([]string{"sha-512=3", "sha-256=10"})
	if err != nil || len(preferences.Entries()) != 2 {
		t.Fatalf("multiline digest-preference seed = %#v, %v", preferences.Entries(), err)
	}

	input := SignatureInput{
		Components: []ComponentIdentifier{{
			Name:       "example",
			Parameters: []Parameter{{Name: "sf", Value: true}},
		}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	multiline := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "example.test", Path: "/"},
		Host:   "example.test",
		Header: http.Header{"Example": []string{"a=1", "b=?1"}},
	}
	combined := multiline.Clone(multiline.Context())
	combined.Header = http.Header{"Example": []string{"a=1, b=?1"}}
	structuredContext := func(request *http.Request) MessageContext {
		return MessageContext{
			Request:          request,
			StructuredFields: map[string]StructuredFieldType{"example": StructuredFieldDictionary},
		}
	}
	multilineBase, multilineErr := CreateSignatureBase(structuredContext(multiline), input)
	combinedBase, combinedErr := CreateSignatureBase(structuredContext(combined), input)
	if multilineErr != nil || combinedErr != nil {
		_, parametersErr := serializeSignatureParameters(input)
		t.Fatalf("valid multiline Structured Field errors = %v, %v; parameters = %v", multilineErr, combinedErr, parametersErr)
	}
	if err := equivalentSignatureBaseResults(multilineBase, multilineErr, combinedBase, combinedErr); err != nil {
		t.Fatal(err)
	}

	externalInput := SignatureInput{
		Components: []ComponentIdentifier{
			{Name: "@scheme"},
			{Name: "@authority"},
			{Name: "@path"},
			{Name: "@query"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	external := &ExternalRequestContext{Scheme: "https", Authority: "Example.COM:443", RequestTarget: "/items/123?view=full"}
	externalBase, err := CreateSignatureBase(MessageContext{
		Request:         fuzzExternalRequest("untrusted.invalid", "host=spoofed.invalid;proto=http", "sha-256=:YWJj:"),
		ExternalRequest: external,
	}, externalInput)
	if err != nil {
		t.Fatalf("valid external/trailer seed error = %v", err)
	}
	const wantExternalBase = `"@scheme": https
"@authority": example.com
"@path": /items/123
"@query": ?view=full
"content-digest";tr: sha-256=:YWJj:
"@signature-params": ("@scheme" "@authority" "@path" "@query" "content-digest";tr);created=1`
	if externalBase != wantExternalBase {
		t.Fatalf("external/trailer signature base =\n%s\nwant =\n%s", externalBase, wantExternalBase)
	}

	raw := []byte("POST /upload HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\nTrailer: Content-Digest, Signature-Input, Signature\r\n\r\n1\r\nx\r\n0\r\nContent-Digest: sha-256=:YWJj:\r\nSignature-Input: sig=(\"content-digest\";tr);created=1\r\nSignature: sig=:YWJj:\r\n\r\n")
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil || !drainFuzzBody(request.Body) {
		t.Fatalf("valid raw trailer seed error = %v", err)
	}
	if _, err := ParseDigestFields(request.Trailer.Values("Content-Digest")); err != nil {
		t.Fatalf("raw trailer Content-Digest error = %v", err)
	}
	if _, err := ParseSignatureInputs(request.Trailer.Values("Signature-Input")); err != nil {
		t.Fatalf("raw trailer Signature-Input error = %v", err)
	}
	if _, err := ParseSignatures(request.Trailer.Values("Signature")); err != nil {
		t.Fatalf("raw trailer Signature error = %v", err)
	}
}

func FuzzParseAcceptSignatures(f *testing.F) {
	f.Add(`sig=("@method" "content-digest");created;expires;keyid="key";alg="hmac-sha256"`, "", false)
	f.Add(`one=("@method");created`, `two=("@status");expires;tag="response"`, true)
	f.Add(`sig=("@method")`, `sig=("@path")`, true)
	f.Add(`sig=("@method");created=1`, "", false)
	f.Add(`sig=("@method`, `other=("@path")`, true)

	f.Fuzz(func(t *testing.T, first, second string, split bool) {
		if !withinFuzzFieldLimit(first, second) {
			return
		}
		values := []string{first + second}
		if split {
			values = []string{first, second}
		}
		parsed, err := ParseAcceptSignatures(values)
		if err != nil {
			return
		}
		canonical, err := ParseAcceptSignatures([]string{parsed.String()})
		if err != nil {
			t.Fatalf("canonical Accept-Signature rejected: %v", err)
		}
		if err := equivalentAcceptSignatures(parsed, canonical); err != nil {
			t.Fatal(err)
		}
		repeated, err := ParseAcceptSignatures(values)
		if err != nil {
			t.Fatalf("repeated Accept-Signature parse rejected: %v", err)
		}
		if err := equivalentAcceptSignatures(parsed, repeated); err != nil {
			t.Fatalf("nondeterministic Accept-Signature parse: %v", err)
		}
	})
}

func FuzzParseDigestPreferences(f *testing.F) {
	f.Add("sha-512=3, sha-256=10, unixsum=0", "", false)
	f.Add("sha-512=3", "sha-256=10", true)
	f.Add("sha-256=1", "sha-256=2", true)
	f.Add("sha-256=11", "", false)
	f.Add("sha-256=1;parameter", "unixsum=0", true)

	f.Fuzz(func(t *testing.T, first, second string, split bool) {
		if !withinFuzzFieldLimit(first, second) {
			return
		}
		values := []string{first + second}
		if split {
			values = []string{first, second}
		}
		parsed, err := ParseDigestPreferences(values)
		if err != nil {
			return
		}
		canonical, err := ParseDigestPreferences([]string{parsed.String()})
		if err != nil {
			t.Fatalf("canonical digest preferences rejected: %v", err)
		}
		if err := equivalentDigestPreferences(parsed, canonical); err != nil {
			t.Fatal(err)
		}
		repeated, err := ParseDigestPreferences(values)
		if err != nil {
			t.Fatalf("repeated digest preferences parse rejected: %v", err)
		}
		if err := equivalentDigestPreferences(parsed, repeated); err != nil {
			t.Fatalf("nondeterministic digest preferences parse: %v", err)
		}
	})
}

func FuzzStrictStructuredFieldsMultiline(f *testing.F) {
	f.Add(uint8(0), "a=1", "b=?1")
	f.Add(uint8(1), `"first"`, `("second" 2)`)
	f.Add(uint8(2), `"item"`, "?1")
	f.Add(uint8(0), `a=%"00000000000000"0000`, "b=1")
	f.Add(uint8(1), "@1", "token")

	f.Fuzz(func(t *testing.T, fieldType uint8, first, second string) {
		if !withinFuzzFieldLimit(first, second) {
			return
		}
		structuredType := StructuredFieldType(fieldType%3 + 1)
		input := SignatureInput{
			Components: []ComponentIdentifier{{
				Name:       "example",
				Parameters: []Parameter{{Name: "sf", Value: true}},
			}},
			Parameters: []Parameter{{Name: "created", Value: int64(1)}},
		}
		multiline := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Scheme: "https", Host: "example.test", Path: "/"},
			Host:   "example.test",
			Header: http.Header{"Example": []string{first, second}},
		}
		normalizedFirst, firstErr := normalizeFieldValue(first)
		normalizedSecond, secondErr := normalizeFieldValue(second)
		if firstErr != nil || secondErr != nil {
			return
		}
		combined := multiline.Clone(multiline.Context())
		combined.Header = http.Header{"Example": []string{normalizedFirst + ", " + normalizedSecond}}
		contextFor := func(request *http.Request) MessageContext {
			return MessageContext{
				Request: request,
				StructuredFields: map[string]StructuredFieldType{
					"example": structuredType,
				},
			}
		}
		multilineBase, multilineErr := CreateSignatureBase(contextFor(multiline), input)
		combinedBase, combinedErr := CreateSignatureBase(contextFor(combined), input)
		if err := equivalentSignatureBaseResults(multilineBase, multilineErr, combinedBase, combinedErr); err != nil {
			t.Fatalf("multiline Structured Field differs from its combined form: %v", err)
		}
	})
}

func FuzzExternalRequestTrailers(f *testing.F) {
	f.Add("https", "Example.COM:443", "/items/123?view=full", "sha-256=:YWJj:")
	f.Add("http", "[2001:db8::1]:80", "/", "sha-512=:ZGVm:")
	f.Add("https", "example.test", "https://example.test/absolute?x=1", "sha-256=:YWJj:")
	f.Add("ftp", "bad host", "not-a-target", "bad\r\nvalue")

	f.Fuzz(func(t *testing.T, scheme, authority, target, trailer string) {
		if !withinFuzzFieldLimit(scheme, authority, target, trailer) {
			return
		}
		external := &ExternalRequestContext{Scheme: scheme, Authority: authority, RequestTarget: target}
		input := SignatureInput{
			Components: []ComponentIdentifier{
				{Name: "@scheme"},
				{Name: "@authority"},
				{Name: "@path"},
				{Name: "@query"},
				{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
			},
			Parameters: []Parameter{{Name: "created", Value: int64(1)}},
		}
		first := fuzzExternalRequest("untrusted-one.invalid", "for=192.0.2.1;host=spoofed.invalid;proto=http", trailer)
		second := fuzzExternalRequest("untrusted-two.invalid", "for=192.0.2.2;host=other.invalid;proto=https", trailer)
		firstBase, firstErr := CreateSignatureBase(MessageContext{Request: first, ExternalRequest: external}, input)
		secondBase, secondErr := CreateSignatureBase(MessageContext{Request: second, ExternalRequest: external}, input)
		if err := equivalentSignatureBaseResults(firstBase, firstErr, secondBase, secondErr); err != nil {
			t.Fatalf("trusted external context was affected by untrusted origin data: %v", err)
		}
	})
}

func FuzzRawHTTPMessageFields(f *testing.F) {
	f.Add(false, []byte("POST /items?x=1 HTTP/1.1\r\nHost: example.test\r\nContent-Length: 0\r\nAccept-Signature: sig=(\"@method\");created;keyid=\"key\"\r\nWant-Content-Digest: sha-256=10, sha-512=3\r\nSignature-Input: sig=(\"@method\");created=1\r\nSignature: sig=:YWJj:\r\nContent-Digest: sha-256=:YWJj:\r\n\r\n"))
	f.Add(false, []byte("POST / HTTP/1.1\r\nHost: example.test\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\nx"))
	f.Add(false, []byte("POST /upload HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\nTrailer: Content-Digest, Signature-Input, Signature\r\n\r\n1\r\nx\r\n0\r\nContent-Digest: sha-256=:YWJj:\r\nSignature-Input: sig=(\"content-digest\";tr);created=1\r\nSignature: sig=:YWJj:\r\n\r\n"))
	f.Add(true, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nAccept-Signature: response=(\"@status\");created\r\nWant-Repr-Digest: sha-256=10\r\nRepr-Digest: sha-256=:YWJj:\r\nSignature-Input: response=(\"@status\");created=1\r\nSignature: response=:YWJj:\r\n\r\n"))
	f.Add(true, []byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nTrailer: Content-Digest\r\n\r\nxyz\r\n"))
	f.Add(false, []byte("GET / HTTP/1.1\r\nHost: example.test\r\nSignature-Input: sig=(\"@method\")\r\n\t;created=1\r\nX-Nul: \x00\r\n\r\n"))

	f.Fuzz(func(t *testing.T, responseMessage bool, raw []byte) {
		if len(raw) > DefaultSyntaxLimits().MaxFieldBytes {
			return
		}
		reader := bufio.NewReader(bytes.NewReader(raw))
		if responseMessage {
			request, err := http.NewRequest(http.MethodGet, "http://example.test/related", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := http.ReadResponse(reader, request)
			if err != nil {
				return
			}
			if !drainFuzzBody(response.Body) {
				return
			}
			exerciseFuzzHTTPFields(t, MessageContext{Response: response, RelatedRequest: request}, response.Header)
			exerciseFuzzHTTPFields(t, MessageContext{Response: response, RelatedRequest: request}, response.Trailer)
			return
		}

		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		if !drainFuzzBody(request.Body) {
			return
		}
		exerciseFuzzHTTPFields(t, MessageContext{Request: request}, request.Header)
		exerciseFuzzHTTPFields(t, MessageContext{Request: request}, request.Trailer)
	})
}

func equivalentAcceptSignatures(left, right AcceptSignatures) error {
	if left.String() != right.String() || !reflect.DeepEqual(left.Entries(), right.Entries()) {
		return fmt.Errorf("Accept-Signature mismatch: %q %#v != %q %#v", left.String(), left.Entries(), right.String(), right.Entries())
	}
	return nil
}

func equivalentDigestPreferences(left, right DigestPreferences) error {
	if left.String() != right.String() || !reflect.DeepEqual(left.Entries(), right.Entries()) {
		return fmt.Errorf("digest preferences mismatch: %q %#v != %q %#v", left.String(), left.Entries(), right.String(), right.Entries())
	}
	return nil
}

func equivalentSignatureBaseResults(left string, leftErr error, right string, rightErr error) error {
	if (leftErr == nil) != (rightErr == nil) {
		return fmt.Errorf("result classification mismatch: %q, %v != %q, %v", left, leftErr, right, rightErr)
	}
	if leftErr == nil && left != right {
		return fmt.Errorf("canonical base mismatch: %q != %q", left, right)
	}
	return nil
}

func withinFuzzFieldLimit(values ...string) bool {
	total := 0
	for _, value := range values {
		total += len(value)
		if total > DefaultSyntaxLimits().MaxFieldBytes {
			return false
		}
	}
	return true
}

func fuzzExternalRequest(host, forwarded, trailer string) *http.Request {
	request := &http.Request{
		Method:     http.MethodPost,
		URL:        &url.URL{Scheme: "http", Host: host, Path: "/untrusted", RawQuery: "source=untrusted"},
		Host:       host,
		RequestURI: "/untrusted?source=untrusted",
		Header: http.Header{
			"Forwarded":        []string{forwarded},
			"X-Forwarded-Host": []string{host},
		},
		Trailer: http.Header{"Content-Digest": []string{trailer}},
	}
	return request
}

func drainFuzzBody(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	limit := int64(DefaultSyntaxLimits().MaxFieldBytes + 1)
	content, err := io.ReadAll(io.LimitReader(body, limit))
	closeErr := body.Close()
	return err == nil && closeErr == nil && int64(len(content)) < limit
}

func exerciseFuzzHTTPFields(t *testing.T, message MessageContext, header http.Header) {
	t.Helper()
	if values := header.Values("Accept-Signature"); len(values) != 0 {
		parsed, err := ParseAcceptSignatures(values)
		if err == nil {
			canonical, canonicalErr := ParseAcceptSignatures([]string{parsed.String()})
			if canonicalErr != nil {
				t.Fatalf("raw HTTP canonical Accept-Signature rejected: %v", canonicalErr)
			}
			if err := equivalentAcceptSignatures(parsed, canonical); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"Want-Content-Digest", "Want-Repr-Digest"} {
		if values := header.Values(name); len(values) != 0 {
			parsed, err := ParseDigestPreferences(values)
			if err == nil {
				canonical, canonicalErr := ParseDigestPreferences([]string{parsed.String()})
				if canonicalErr != nil {
					t.Fatalf("raw HTTP canonical %s rejected: %v", name, canonicalErr)
				}
				if err := equivalentDigestPreferences(parsed, canonical); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, name := range []string{"Content-Digest", "Repr-Digest"} {
		if values := header.Values(name); len(values) != 0 {
			parsed, err := ParseDigestFields(values)
			if err == nil {
				canonical, canonicalErr := ParseDigestField(parsed.String())
				if canonicalErr != nil || !reflect.DeepEqual(parsed.Entries(), canonical.Entries()) {
					t.Fatalf("raw HTTP %s canonical mismatch: %v", name, canonicalErr)
				}
			}
		}
	}
	if values := header.Values("Signature"); len(values) != 0 {
		parsed, err := ParseSignatures(values)
		if err == nil {
			canonical, canonicalErr := ParseSignatures([]string{parsed.String()})
			if canonicalErr != nil || !reflect.DeepEqual(parsed.Entries(), canonical.Entries()) {
				t.Fatalf("raw HTTP Signature canonical mismatch: %v", canonicalErr)
			}
		}
	}
	if values := header.Values("Signature-Input"); len(values) != 0 {
		parsed, err := ParseSignatureInputs(values)
		if err != nil {
			return
		}
		canonical, canonicalErr := ParseSignatureInputs([]string{parsed.String()})
		if canonicalErr != nil || !reflect.DeepEqual(parsed.Entries(), canonical.Entries()) {
			t.Fatalf("raw HTTP Signature-Input canonical mismatch: %v", canonicalErr)
		}
		for _, input := range parsed.Entries() {
			_, _ = CreateSignatureBase(message, input)
		}
	}
}
