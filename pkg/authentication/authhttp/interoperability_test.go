package authhttp_test

import (
	"net/http"
	"net/url"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

func TestRFC7617BasicCredentialVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		username string
		password string
	}{
		{
			name: "Aladdin example", header: "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==",
			username: "Aladdin", password: "open sesame",
		},
		{
			name: "UTF-8 pound example", header: "Basic dGVzdDoxMjPCow==",
			username: "test", password: "123£",
		},
	}
	extractor := mustExtractor(t, authhttp.BasicAuthorization())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &http.Request{Header: http.Header{"Authorization": {tt.header}}, URL: &url.URL{}}
			credential, err := extractor.Extract(request)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			basic, ok := credential.(authentication.BasicCredential)
			if !ok || basic.Username() != tt.username || basic.Password() != tt.password {
				t.Fatalf("Extract() credential = %T", credential)
			}
		})
	}
}

func TestRFC6750BearerHeaderVector(t *testing.T) {
	t.Parallel()

	request := &http.Request{
		Header: http.Header{"Authorization": {"Bearer mF_9.B5f-4.1JqM"}},
		URL:    &url.URL{},
	}
	credential, err := mustExtractor(t, authhttp.BearerAuthorization()).Extract(request)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	bearer, ok := credential.(authentication.BearerCredential)
	if !ok || bearer.Token() != "mF_9.B5f-4.1JqM" {
		t.Fatalf("Extract() credential = %T", credential)
	}
}
