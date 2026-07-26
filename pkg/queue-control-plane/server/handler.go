package server

import (
	"net/http"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
)

// NewAdministrativeHandler composes browser admission and fail-closed
// authentication middleware around the administrative API. Missing
// credentials become an explicit anonymous principal so health endpoints can
// remain public; administrative endpoints still reject that principal.
func NewAdministrativeHandler(
	api http.Handler,
	extractor authhttp.CredentialExtractor,
	authenticator authentication.Authenticator,
	securityConfig apihttp.SecurityConfig,
) (http.Handler, error) {
	if nilInterface(api) {
		return nil, ErrInvalidConfiguration
	}
	authenticate, err := authhttp.NewMiddleware(
		extractor,
		authenticator,
		authhttp.WithOptionalAnonymous(),
	)
	if err != nil {
		return nil, err
	}
	admitted := api
	if securityConfig.RateLimiter != nil {
		limit, err := apihttp.NewRateLimitMiddleware(securityConfig.RateLimiter)
		if err != nil {
			return nil, err
		}
		admitted = limit(api)
		securityConfig.RateLimiter = nil
	}
	secure, err := apihttp.NewSecurityMiddleware(securityConfig)
	if err != nil {
		return nil, err
	}

	return secure(authenticate(admitted)), nil
}
