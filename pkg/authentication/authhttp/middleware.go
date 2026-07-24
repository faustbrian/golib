package authhttp

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

// CredentialExtractor extracts one typed credential from an HTTP request.
type CredentialExtractor interface {
	Extract(*http.Request) (authentication.Credential, error)
}

type middlewareConfig struct {
	optional   bool
	challenges []string
}

// MiddlewareOption configures HTTP authentication middleware.
type MiddlewareOption func(*middlewareConfig) error

// WithOptionalAnonymous permits anonymous access only when credentials are absent.
func WithOptionalAnonymous() MiddlewareOption {
	return func(configuration *middlewareConfig) error {
		configuration.optional = true
		return nil
	}
}

// WithChallenges configures fallback challenges for authentication failures.
func WithChallenges(challenges ...authentication.Challenge) MiddlewareOption {
	return func(configuration *middlewareConfig) error {
		for _, challenge := range challenges {
			formatted, err := FormatChallenge(challenge)
			if err != nil {
				return fmt.Errorf("%w: HTTP challenge", authentication.ErrInvalidConfiguration)
			}
			configuration.challenges = append(configuration.challenges, formatted)
		}
		return nil
	}
}

// NewMiddleware creates fail-closed authentication-only net/http middleware.
func NewMiddleware(extractor CredentialExtractor, authenticator authentication.Authenticator, options ...MiddlewareOption) (func(http.Handler) http.Handler, error) {
	if isNilInterface(extractor) || isNilInterface(authenticator) {
		return nil, fmt.Errorf("%w: HTTP middleware dependency", authentication.ErrInvalidConfiguration)
	}
	configuration := middlewareConfig{}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			credential, err := extractor.Extract(request)
			if err != nil {
				if configuration.optional && errors.Is(err, authentication.ErrCredentialsAbsent) {
					ctx := authentication.ContextWithPrincipal(request.Context(), authentication.AnonymousPrincipal())
					next.ServeHTTP(writer, request.WithContext(ctx))
					return
				}
				writeFailure(writer, err, configuration.challenges)
				return
			}

			result, err := authenticator.Authenticate(request.Context(), credential)
			if err != nil {
				writeFailure(writer, err, configuration.challenges)
				return
			}
			principal, ok := result.Principal()
			if !ok || result.State() != authentication.ResultAuthenticated {
				writeFailure(writer, authentication.NewFailure(authentication.FailureUnavailable,
					authentication.WithFailureCause(authentication.ErrInvalidConfiguration)), configuration.challenges)
				return
			}

			ctx := authentication.ContextWithPrincipal(request.Context(), principal)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}, nil
}

func writeFailure(writer http.ResponseWriter, err error, fallbackChallenges []string) {
	status := http.StatusUnauthorized
	message := "authentication failed"
	if errors.Is(err, authentication.ErrAuthenticationUnavailable) || !classifiedHTTPFailure(err) {
		status = http.StatusServiceUnavailable
		message = "authentication unavailable"
	}

	if status == http.StatusUnauthorized {
		challenges := failureChallenges(err)
		if len(challenges) == 0 {
			challenges = fallbackChallenges
		}
		for _, challenge := range challenges {
			writer.Header().Add("WWW-Authenticate", challenge)
		}
	}
	http.Error(writer, message, status)
}

func failureChallenges(err error) []string {
	var failure *authentication.Failure
	if !errors.As(err, &failure) {
		return nil
	}
	var formatted []string
	for _, challenge := range failure.Challenges() {
		value, formatErr := FormatChallenge(challenge)
		if formatErr == nil {
			formatted = append(formatted, value)
		}
	}
	return formatted
}

func classifiedHTTPFailure(err error) bool {
	return errors.Is(err, authentication.ErrCredentialsAbsent) ||
		errors.Is(err, authentication.ErrCredentialsInvalid) ||
		errors.Is(err, authentication.ErrCredentialsRejected) ||
		errors.Is(err, authentication.ErrAmbiguousCredentials) ||
		errors.Is(err, authentication.ErrAuthenticationUnavailable)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
