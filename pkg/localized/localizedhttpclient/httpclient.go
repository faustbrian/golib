// Package localizedhttpclient integrates negotiation with http-client.
package localizedhttpclient

import (
	"net/http"
	"strconv"
	"strings"

	httpclient "github.com/faustbrian/golib/pkg/http-client"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedhttp "github.com/faustbrian/golib/pkg/localized/http"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

// Error is a stable privacy-safe HTTP client adapter error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

// ErrInvalidResponse reports a nil response or originating request.
const ErrInvalidResponse Error = "localized http client: invalid response"

// WithPreferences returns a request spec with a canonical Accept-Language
// header at layer. An empty preference list removes the header at that layer.
func WithPreferences(
	spec httpclient.RequestSpec,
	layer httpclient.RequestLayer,
	preferences ...localizedmatch.Preference,
) (httpclient.RequestSpec, error) {
	if len(preferences) == 0 {
		return spec.WithoutHeader(layer, "Accept-Language")
	}
	parts := make([]string, len(preferences))
	for i, preference := range preferences {
		canonical, err := preference.Locale.Canonical()
		if err != nil {
			return httpclient.RequestSpec{}, localizedhttp.ErrInvalidRange
		}
		part := canonical.String()
		if preference.Weight != 1 {
			part += ";q=" + strconv.FormatFloat(preference.Weight, 'f', -1, 64)
		}
		parts[i] = part
	}
	header := strings.Join(parts, ", ")
	if _, err := localizedhttp.ParseAcceptLanguage(header, localizedhttp.ParseOptions{
		MaxBytes: len(header), MaxCandidates: len(preferences),
	}); err != nil {
		return httpclient.RequestSpec{}, err
	}
	return spec.WithHeader(layer, "Accept-Language", header)
}

// SelectResponse selects a localized value from the Accept-Language header on
// the response's originating request. It applies matching but no fallback.
func SelectResponse(
	value localized.Text,
	response *http.Response,
	options localizedhttp.ParseOptions,
) (localizedmatch.Result, error) {
	if response == nil || response.Request == nil {
		return localizedmatch.Result{}, ErrInvalidResponse
	}
	return localizedhttp.Select(value, response.Request.Header.Get("Accept-Language"), options)
}
