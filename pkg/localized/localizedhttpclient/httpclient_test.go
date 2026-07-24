package localizedhttpclient_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	httpclient "github.com/faustbrian/golib/pkg/http-client"
	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedhttp "github.com/faustbrian/golib/pkg/localized/http"
	"github.com/faustbrian/golib/pkg/localized/localizedhttpclient"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func TestPreferencesApplyCanonicalAcceptLanguageHeader(t *testing.T) {
	t.Parallel()

	spec, err := httpclient.NewRequestSpec("https://example.test", "/content")
	if err != nil {
		t.Fatal(err)
	}
	english, _ := locale.Parse("EN-us")
	finnish, _ := locale.Parse("fi")
	spec, err = localizedhttpclient.WithPreferences(spec, httpclient.LayerRequest,
		localizedmatch.Preference{Locale: english, Weight: 1},
		localizedmatch.Preference{Locale: finnish, Weight: 0.7},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Accept-Language"); got != "en-US, fi;q=0.7" {
		t.Fatalf("Accept-Language = %q", got)
	}

	value, _ := localized.TextFromMap(map[string]string{"en-US": "Hello", "fi": "Hei"})
	result, err := localizedhttpclient.SelectResponse(value, &http.Response{Request: request}, localizedhttp.ParseOptions{})
	if err != nil || result.Locale.String() != "en-US" || result.Text != "Hello" {
		t.Fatalf("SelectResponse() = %+v, %v", result, err)
	}
}

func TestHTTPClientAdapterRejectsInvalidAndSupportsRemoval(t *testing.T) {
	t.Parallel()
	if got := localizedhttpclient.ErrInvalidResponse.Error(); got != "localized http client: invalid response" {
		t.Fatalf("Error() = %q", got)
	}

	spec, _ := httpclient.NewRequestSpec("https://example.test", "/")
	zero := locale.Tag{}
	if _, err := localizedhttpclient.WithPreferences(spec, httpclient.LayerRequest,
		localizedmatch.Preference{Locale: zero, Weight: 1}); !errors.Is(err, localizedhttp.ErrInvalidRange) {
		t.Fatalf("zero preference error = %v", err)
	}
	english, _ := locale.Parse("en")
	if _, err := localizedhttpclient.WithPreferences(spec, httpclient.LayerRequest,
		localizedmatch.Preference{Locale: english, Weight: 0.1234}); !errors.Is(err, localizedhttp.ErrInvalidWeight) {
		t.Fatalf("precision error = %v", err)
	}
	if _, err := localizedhttpclient.SelectResponse(localized.Text{}, nil, localizedhttp.ParseOptions{}); !errors.Is(err, localizedhttpclient.ErrInvalidResponse) {
		t.Fatalf("nil response error = %v", err)
	}

	spec, err := localizedhttpclient.WithPreferences(spec, httpclient.LayerRequest)
	if err != nil {
		t.Fatal(err)
	}
	request, err := spec.Build(context.Background(), http.MethodGet)
	if err != nil || request.Header.Get("Accept-Language") != "" {
		t.Fatalf("removed header = %q, %v", request.Header.Get("Accept-Language"), err)
	}
}
