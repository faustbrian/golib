package httpsignature

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCreateSignatureBaseMatchesRFC9421RequestExample(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("POST", "https://example.com/foo?param=Value&Pet=dog", strings.NewReader(`{"hello": "world"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Digest", "sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Length", "18")

	inputs, err := ParseSignatureInputs([]string{`sig1=("@method" "@authority" "@path" "@query" "content-digest" "content-type" "content-length");created=1618884475;keyid="test-key-rsa-pss"`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}

	const want = `"@method": POST
"@authority": example.com
"@path": /foo
"@query": ?param=Value&Pet=dog
"content-digest": sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:
"content-type": application/json
"content-length": 18
"@signature-params": ("@method" "@authority" "@path" "@query" "content-digest" "content-type" "content-length");created=1618884475;keyid="test-key-rsa-pss"`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBaseNormalizesOriginWithoutTrustingForwardedFields(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("custom", "HTTPS://EXAMPLE.COM:443/a%2Fb?x=%2f", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Forwarded", "proto=http;host=attacker.example")

	inputs, err := ParseSignatureInputs([]string{`sig=("@method" "@scheme" "@authority" "@target-uri" "@request-target" "@path" "@query")`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}

	const want = `"@method": custom
"@scheme": https
"@authority": example.com
"@target-uri": https://example.com/a%2Fb?x=%2f
"@request-target": /a%2Fb?x=%2f
"@path": /a%2Fb
"@query": ?x=%2f
"@signature-params": ("@method" "@scheme" "@authority" "@target-uri" "@request-target" "@path" "@query")`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBaseOmitsNumericallyDefaultPort(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://EXAMPLE.COM:00443/", nil)
	inputs, _ := ParseSignatureInputs([]string{`sig=("@authority" "@target-uri")`})
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"@authority": example.com
"@target-uri": https://example.com/
"@signature-params": ("@authority" "@target-uri")`
	if base != want {
		t.Fatalf("signature base = %q, want %q", base, want)
	}
}

func TestCreateSignatureBaseCombinesFieldInstancesAndSeparatesTrailers(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header["X-Example"] = []string{" first ", "second\r\n\tline"}
	request.Trailer = http.Header{"X-Example": []string{" trailer "}}

	inputs, err := ParseSignatureInputs([]string{`sig=("x-example" "x-example";tr)`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}

	const want = `"x-example": first, second line
"x-example";tr: trailer
"@signature-params": ("x-example" "x-example";tr)`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBaseCanonicalizesKnownStructuredFields(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	request.Header["Example-Dictionary"] = []string{"a=1,b=?0"}
	request.Header["Example-List"] = []string{"token;flag,(1 2)"}
	request.Header["Example-Item"] = []string{"\"value\";flag"}
	inputs, err := ParseSignatureInputs([]string{`sig=("example-dictionary";sf "example-dictionary";key="b" "example-list";sf "example-item";sf)`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{
		Request: request,
		StructuredFields: map[string]StructuredFieldType{
			"example-dictionary": StructuredFieldDictionary,
			"example-list":       StructuredFieldList,
			"example-item":       StructuredFieldItem,
		},
	}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"example-dictionary";sf: a=1, b=?0
"example-dictionary";key="b": ?0
"example-list";sf: token;flag, (1 2)
"example-item";sf: "value";flag
"@signature-params": ("example-dictionary";sf "example-dictionary";key="b" "example-list";sf "example-item";sf)`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBaseRejectsMultipleLinesForStructuredItem(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	request.Header["Example-Item"] = []string{"one", "two"}
	inputs, _ := ParseSignatureInputs([]string{`sig=("example-item";sf)`})
	_, err := CreateSignatureBase(MessageContext{
		Request:          request,
		StructuredFields: map[string]StructuredFieldType{"example-item": StructuredFieldItem},
	}, inputs.Entries()[0])
	if !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

func TestCreateSignatureBaseBinaryWrapsNonASCIIFieldOctets(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	request.Header["X-Raw"] = []string{" \xff\x00 "}
	inputs, err := ParseSignatureInputs([]string{`sig=("x-raw";bs)`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"x-raw";bs: :/wA=:
"@signature-params": ("x-raw";bs)`
	if base != want {
		t.Fatalf("signature base = %q, want %q", base, want)
	}
}

func TestCreateSignatureBaseRejectsDerivedWhitespaceBoundaries(t *testing.T) {
	t.Parallel()

	request := &http.Request{Method: " GET", URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/"}, Header: make(http.Header)}
	inputs, _ := ParseSignatureInputs([]string{`sig=("@method")`})
	if _, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0]); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
}

func TestCreateSignatureBaseRejectsUnresolvableOrIncompatibleComponents(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	for _, input := range []string{
		`sig=("missing")`,
		`sig=("@unknown")`,
		`sig=("@status")`,
		`sig=("@method";req)`,
		`sig=("x";bs;sf)`,
		`sig=("@signature-params")`,
	} {
		input := input

		t.Run(input, func(t *testing.T) {
			t.Parallel()

			inputs, parseErr := ParseSignatureInputs([]string{input})
			if parseErr != nil {
				t.Fatalf("ParseSignatureInputs() error = %v", parseErr)
			}
			_, baseErr := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
			if !errors.Is(baseErr, ErrSignatureBase) {
				t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", baseErr)
			}
		})
	}
}

func TestCreateSignatureBaseAllowsAnEmptyCoveredSet(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	inputs, err := ParseSignatureInputs([]string{`sig=();nonce="unique"`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	if base != `"@signature-params": ();nonce="unique"` {
		t.Fatalf("signature base = %q", base)
	}
}

func TestCreateSignatureBaseUsesExplicitExternalRequestTargetThroughout(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "http://internal.example/internal?wrong=1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	inputs, err := ParseSignatureInputs([]string{`sig=("@target-uri" "@request-target" "@path" "@query")`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{
		Request: request,
		ExternalRequest: &ExternalRequestContext{
			Scheme:        "https",
			Authority:     "EXAMPLE.COM:443",
			RequestTarget: "/public%2Fpath?right=%2f",
		},
	}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}

	const want = `"@target-uri": https://example.com/public%2Fpath?right=%2f
"@request-target": /public%2Fpath?right=%2f
"@path": /public%2Fpath
"@query": ?right=%2f
"@signature-params": ("@target-uri" "@request-target" "@path" "@query")`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBaseRejectsPartialExternalRequestContext(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "http://internal.example/internal", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "@target-uri"}}}

	_, err = CreateSignatureBase(MessageContext{
		Request:         request,
		ExternalRequest: &ExternalRequestContext{Scheme: "https"},
	}, input)
	if !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

func TestCreateSignatureBaseRejectsContradictoryExternalAbsoluteTarget(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "http://internal.example/", nil)
	inputs, _ := ParseSignatureInputs([]string{`sig=("@target-uri")`})
	_, err := CreateSignatureBase(MessageContext{
		Request: request,
		ExternalRequest: &ExternalRequestContext{
			Scheme:        "https",
			Authority:     "example.com",
			RequestTarget: "http://attacker.example/path",
		},
	}, inputs.Entries()[0])
	if !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

func TestCreateSignatureBaseRejectsRequestTargetsOutsideHTTPGrammar(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/path#fragment", "https://user@example.com/path"} {
		request := &http.Request{
			Method:     http.MethodGet,
			URL:        &url.URL{},
			Host:       "example.com",
			RequestURI: target,
			Header:     make(http.Header),
		}
		inputs, _ := ParseSignatureInputs([]string{`sig=("@target-uri")`})
		if _, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0]); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase(%q) error = %v, want ErrSignatureBase", target, err)
		}
	}
}

func TestCreateSignatureBasePreservesDoubleSlashOriginFormPath(t *testing.T) {
	t.Parallel()

	request := &http.Request{
		Method:     http.MethodGet,
		URL:        &url.URL{},
		Host:       "example.com",
		RequestURI: "//not-an-authority/path?x=1",
		Header:     make(http.Header),
	}
	inputs, _ := ParseSignatureInputs([]string{`sig=("@path" "@query")`})
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"@path": //not-an-authority/path
"@query": ?x=1
"@signature-params": ("@path" "@query")`
	if base != want {
		t.Fatalf("signature base = %q, want %q", base, want)
	}
}

func TestCreateSignatureBaseQueryParamUsesUTF8ReplacementFromHTMLFormParsing(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.com/?%FF=%FF", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	inputs, err := ParseSignatureInputs([]string{`sig=("@query-param";name="%EF%BF%BD")`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"@query-param";name="%EF%BF%BD": %EF%BF%BD
"@signature-params": ("@query-param";name="%EF%BF%BD")`
	if base != want {
		t.Fatalf("signature base = %q, want %q", base, want)
	}
}

func TestCreateSignatureBaseUsesAbsoluteFormRequestTargetOrigin(t *testing.T) {
	t.Parallel()

	request := &http.Request{
		Method:     "GET",
		URL:        &url.URL{Path: "/ignored"},
		Host:       "internal.example",
		RequestURI: "HTTPS://EXAMPLE.COM:443/proxy%2Fpath?x=%2f",
		Header:     make(http.Header),
	}
	inputs, err := ParseSignatureInputs([]string{`sig=("@scheme" "@authority" "@target-uri" "@path" "@query")`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"@scheme": https
"@authority": example.com
"@target-uri": https://example.com/proxy%2Fpath?x=%2f
"@path": /proxy%2Fpath
"@query": ?x=%2f
"@signature-params": ("@scheme" "@authority" "@target-uri" "@path" "@query")`
	if base != want {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, want)
	}
}

func TestCreateSignatureBasePreservesAuthorityAndAsteriskRequestTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    *http.Request
		wantTarget string
	}{
		{
			name:       "authority",
			request:    &http.Request{Method: http.MethodConnect, URL: &url.URL{}, Host: "www.example.com", RequestURI: "www.example.com:80", Header: make(http.Header)},
			wantTarget: "www.example.com:80",
		},
		{
			name:       "asterisk",
			request:    &http.Request{Method: http.MethodOptions, URL: &url.URL{Path: "*"}, Host: "www.example.com", RequestURI: "*", Header: make(http.Header)},
			wantTarget: "*",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inputs, _ := ParseSignatureInputs([]string{`sig=("@request-target")`})
			base, err := CreateSignatureBase(MessageContext{Request: test.request}, inputs.Entries()[0])
			if err != nil {
				t.Fatalf("CreateSignatureBase() error = %v", err)
			}
			want := `"@request-target": ` + test.wantTarget + "\n" + `"@signature-params": ("@request-target")`
			if base != want {
				t.Fatalf("signature base = %q, want %q", base, want)
			}
		})
	}
}

func TestCreateSignatureBaseReconstructsSpecialFormTargetURIAndPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request *http.Request
		want    string
	}{
		{
			name: "authority",
			request: &http.Request{Method: http.MethodConnect, URL: &url.URL{}, Host: "www.example.com",
				RequestURI: "www.example.com:80", Header: make(http.Header)},
			want: `"@target-uri": http://www.example.com
"@path": /
"@query": ?
"@signature-params": ("@target-uri" "@path" "@query")`,
		},
		{
			name: "asterisk",
			request: &http.Request{Method: http.MethodOptions, URL: &url.URL{Path: "*"}, Host: "www.example.com",
				RequestURI: "*", Header: make(http.Header)},
			want: `"@target-uri": http://www.example.com
"@path": /
"@query": ?
"@signature-params": ("@target-uri" "@path" "@query")`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inputs, _ := ParseSignatureInputs([]string{`sig=("@target-uri" "@path" "@query")`})
			base, err := CreateSignatureBase(MessageContext{Request: test.request}, inputs.Entries()[0])
			if err != nil {
				t.Fatalf("CreateSignatureBase() error = %v", err)
			}
			if base != test.want {
				t.Fatalf("signature base = %q, want %q", base, test.want)
			}
		})
	}
}

func TestCreateSignatureBaseRejectsAsteriskFormOutsideOptions(t *testing.T) {
	t.Parallel()

	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "*"}, Host: "example.com", RequestURI: "*", Header: make(http.Header)}
	inputs, _ := ParseSignatureInputs([]string{`sig=("@request-target")`})
	if _, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0]); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

func TestCreateSignatureBaseUsesAuthorityFormForConnectTargetURI(t *testing.T) {
	t.Parallel()

	request := &http.Request{
		Method:     http.MethodConnect,
		URL:        &url.URL{},
		Host:       "proxy.example",
		RequestURI: "tunnel.example:8443",
		Header:     make(http.Header),
	}
	inputs, _ := ParseSignatureInputs([]string{`sig=("@target-uri" "@authority")`})
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	const want = `"@target-uri": http://tunnel.example:8443
"@authority": tunnel.example:8443
"@signature-params": ("@target-uri" "@authority")`
	if base != want {
		t.Fatalf("signature base = %q, want %q", base, want)
	}
}

func TestCreateSignatureBaseRejectsMalformedAuthorityForm(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"user@tunnel.example:443", "tunnel.example:443?query", "tunnel.example"} {
		request := &http.Request{Method: http.MethodConnect, URL: &url.URL{}, Host: "proxy.example", RequestURI: target, Header: make(http.Header)}
		inputs, _ := ParseSignatureInputs([]string{`sig=("@target-uri")`})
		if _, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0]); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase(%q) error = %v, want ErrSignatureBase", target, err)
		}
	}
}

func TestSignatureBaseIsStableAcrossHTTPVersionsVisibleThroughNetHTTP(t *testing.T) {
	t.Parallel()

	inputs, err := ParseSignatureInputs([]string{`sig=("@method" "@authority" "@path" "x-example")`})
	if err != nil {
		t.Fatal(err)
	}
	var baseline string
	for _, version := range []struct {
		proto        string
		major, minor int
	}{
		{"HTTP/1.1", 1, 1},
		{"HTTP/2.0", 2, 0},
		{"HTTP/3.0", 3, 0},
	} {
		request, requestErr := http.NewRequest(http.MethodGet, "https://example.com/a%2Fb?x=1", nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Proto, request.ProtoMajor, request.ProtoMinor = version.proto, version.major, version.minor
		request.RequestURI = "/a%2Fb?x=1"
		request.Header.Set("X-Example", "one")
		base, baseErr := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
		if baseErr != nil {
			t.Fatalf("%s base error = %v", version.proto, baseErr)
		}
		if baseline == "" {
			baseline = base
			continue
		}
		if base != baseline {
			t.Fatalf("%s changed the net/http-visible signature base\n%s\nwant\n%s", version.proto, base, baseline)
		}
	}
}

func TestCreateSignatureBaseRejectsAuthorityWithQueryOrFragment(t *testing.T) {
	t.Parallel()

	inputs, _ := ParseSignatureInputs([]string{`sig=("@authority")`})
	for _, authority := range []string{"example.com?admin=true", "example.com#fragment"} {
		request := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
		request.Host = authority
		request.URL.Host = ""
		request.RequestURI = "/"
		if _, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0]); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase(%q) error = %v, want ErrSignatureBase", authority, err)
		}
	}
}

func TestCreateSignatureBaseReturnsErrorsForCallerBuiltInvalidValues(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	invalidParameter := SignatureInput{
		Components: []ComponentIdentifier{{
			Name:       "@method",
			Parameters: []Parameter{{Name: "extension", Value: struct{}{}}},
		}},
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, invalidParameter); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase(invalid parameter) error = %v, want ErrSignatureBase", err)
	}

	request.Method = "GE\x00T"
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}}}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase(control byte) error = %v, want ErrSignatureBase", err)
	}
}
