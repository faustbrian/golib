package authhttp_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

func BenchmarkBearerAuthorizationExtraction(b *testing.B) {
	extractor, err := authhttp.NewExtractor(authhttp.BearerAuthorization())
	if err != nil {
		b.Fatalf("NewExtractor() error = %v", err)
	}
	request := &http.Request{Header: http.Header{"Authorization": {"Bearer opaque-token"}}, URL: &url.URL{}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := extractor.Extract(request); err != nil {
			b.Fatal(err)
		}
	}
}
