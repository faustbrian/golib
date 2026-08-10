package httpsignature

import (
	"net/http"
	"net/url"
	"testing"
)

func FuzzParseSignatureInputs(f *testing.F) {
	for _, seed := range []string{
		`sig=("@method" "@authority");created=1618884473;keyid="test-key-rsa-pss"`,
		`sig=()`,
		`sig=("content-digest";sf)`,
		`sig=("@method"), sig=("@path")`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		parsed, err := ParseSignatureInputs([]string{value})
		if err != nil {
			return
		}
		serialized := parsed.String()
		if _, err := ParseSignatureInputs([]string{serialized}); err != nil {
			t.Fatalf("canonical serialization rejected: %v", err)
		}
	})
}

func FuzzParseSignatures(f *testing.F) {
	for _, seed := range []string{`sig=:YWJj:`, `one=:YWJj:, two=:ZGVm:`, `sig="wrong"`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		parsed, err := ParseSignatures([]string{value})
		if err != nil {
			return
		}
		if _, err := ParseSignatures([]string{parsed.String()}); err != nil {
			t.Fatalf("canonical serialization rejected: %v", err)
		}
	})
}

func FuzzParseDigestFields(f *testing.F) {
	for _, seed := range []string{
		`sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:`,
		`sha-512=:YWJj:, sha-256=:ZGVm:`,
		`sha-256="wrong"`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		parsed, err := ParseDigestField(value)
		if err != nil {
			return
		}
		if _, err := ParseDigestField(parsed.String()); err != nil {
			t.Fatalf("canonical serialization rejected: %v", err)
		}
	})
}

func FuzzCreateSignatureBase(f *testing.F) {
	f.Add("GET", "/path", "x=1", "value", `sig=("@method" "@path" "@query" "x-example")`)
	f.Add("CONNECT", "", "", "value", `sig=("@request-target")`)
	f.Fuzz(func(t *testing.T, method, path, rawQuery, fieldValue, inputValue string) {
		if len(method)+len(path)+len(rawQuery)+len(fieldValue)+len(inputValue) > DefaultSyntaxLimits().MaxFieldBytes {
			return
		}
		inputs, err := ParseSignatureInputs([]string{inputValue})
		if err != nil || len(inputs.entries) != 1 {
			return
		}
		request := &http.Request{
			Method: method,
			URL:    &url.URL{Scheme: "https", Host: "example.com", Path: path, RawQuery: rawQuery},
			Host:   "example.com",
			Header: http.Header{"X-Example": []string{fieldValue}},
		}
		request.RequestURI = request.URL.RequestURI()
		_, _ = CreateSignatureBase(MessageContext{Request: request}, inputs.entries[0])
	})
}
