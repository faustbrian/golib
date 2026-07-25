package authhttp_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

func FuzzAuthorizationExtraction(f *testing.F) {
	f.Add("Bearer token")
	f.Add("Basic dXNlcjpwYXNzd29yZA==")
	f.Add("Bearer token token")
	extractor := mustFuzzExtractor(f, authhttp.BasicAuthorization(), authhttp.BearerAuthorization())
	f.Fuzz(func(t *testing.T, header string) {
		if len(header) > 64*1024 {
			t.Skip()
		}
		request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
		request.Header.Set("Authorization", header)
		_, err := extractor.Extract(request)
		if err != nil && !classifiedFuzzError(err) {
			t.Fatalf("unclassified error: %v", err)
		}
	})
}

func FuzzCredentialHeaderSet(f *testing.F) {
	f.Add("Bearer token", "", "primary", "key", "Basic cHJveHk6c2VjcmV0", uint8(0b00101))
	f.Add("", "", "", "", "", uint8(0))
	f.Add("Basic dXNlcjpwYXNzd29yZA==", "Bearer token", "id", "key", "", uint8(0b01111))
	extractor := mustFuzzExtractor(f,
		authhttp.BasicAuthorization(),
		authhttp.BearerAuthorization(),
		authhttp.APIKeyHeader("X-API-Key-ID", "X-API-Key"),
	)
	f.Fuzz(func(t *testing.T, first, second, keyID, key, proxy string, flags uint8) {
		if len(first)+len(second)+len(keyID)+len(key)+len(proxy) > 64*1024 {
			t.Skip()
		}
		request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
		if flags&1 != 0 {
			request.Header.Add("authorization", first)
		}
		if flags&2 != 0 {
			request.Header.Add("Authorization", second)
		}
		if flags&4 != 0 {
			request.Header.Add("x-api-key-id", keyID)
		}
		if flags&8 != 0 {
			request.Header.Add("X-API-Key", key)
		}
		if flags&16 != 0 {
			request.Header.Add("Proxy-Authorization", proxy)
		}

		credential, err := extractor.Extract(request)
		if err != nil {
			if !classifiedFuzzError(err) {
				t.Fatalf("unclassified error: %v", err)
			}
			return
		}
		formatted := fmt.Sprintf("%v %#v", credential, credential)
		switch typed := credential.(type) {
		case authentication.BasicCredential:
			if formatted != "basic credential [REDACTED] authentication.BasicCredential{[REDACTED]}" {
				t.Fatalf("unexpected Basic credential formatting for password length %d", len(typed.Password()))
			}
		case authentication.BearerCredential:
			if formatted != "bearer credential [REDACTED] authentication.BearerCredential{[REDACTED]}" {
				t.Fatalf("unexpected bearer credential formatting for token length %d", len(typed.Token()))
			}
		case authentication.APIKeyCredential:
			if formatted != "api-key credential [REDACTED] authentication.APIKeyCredential{[REDACTED]}" {
				t.Fatalf("unexpected API-key credential formatting for key length %d", len(typed.Key()))
			}
		default:
			t.Fatalf("unexpected credential type %T", credential)
		}
	})
}

func FuzzChallengeFormatting(f *testing.F) {
	f.Add("api")
	f.Add("a\"b\\c")
	f.Add("bad\r\nvalue")
	f.Add("snowman ☃")
	f.Fuzz(func(t *testing.T, realm string) {
		if len(realm) > 64*1024 {
			t.Skip()
		}
		challenge, err := authentication.NewChallenge("Bearer", map[string]string{"realm": realm})
		if err != nil {
			return
		}
		formatted, err := authhttp.FormatChallenge(challenge)
		if err != nil {
			t.Fatalf("FormatChallenge() error = %v", err)
		}
		if strings.ContainsAny(formatted, "\r\n\x00") {
			t.Fatal("formatted challenge contains a field-breaking control")
		}
	})
}

func FuzzBearerQueryExtraction(f *testing.F) {
	f.Add("access_token=token")
	f.Add("access_token=one&access_token=two")
	f.Add("%")
	extractor := mustFuzzExtractor(f, authhttp.BearerQuery("access_token"))
	f.Fuzz(func(t *testing.T, rawQuery string) {
		if len(rawQuery) > 64*1024 {
			t.Skip()
		}
		request := &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: rawQuery}}
		_, err := extractor.Extract(request)
		if err != nil && !classifiedFuzzError(err) {
			t.Fatalf("unclassified error: %v", err)
		}
	})
}

func mustFuzzExtractor(f *testing.F, sources ...authhttp.Source) *authhttp.Extractor {
	f.Helper()
	extractor, err := authhttp.NewExtractor(sources...)
	if err != nil {
		f.Fatalf("NewExtractor() error = %v", err)
	}
	return extractor
}

func classifiedFuzzError(err error) bool {
	return errors.Is(err, authentication.ErrCredentialsAbsent) ||
		errors.Is(err, authentication.ErrCredentialsInvalid) ||
		errors.Is(err, authentication.ErrAmbiguousCredentials)
}
