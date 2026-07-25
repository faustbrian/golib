package webhook

import (
	"context"
	"fmt"
	"net/http"
)

// ErrorHook receives internal verification failures. Implementations must not
// log request bodies, signatures, secrets, or unredacted sensitive headers.
type ErrorHook func(ctx context.Context, err error)

// MiddlewareConfig configures inbound verification middleware.
type MiddlewareConfig struct {
	Request       RequestOptions
	FailureStatus int
	OnError       ErrorHook
}

type verificationContextKey struct{}
type verifiedBodyContextKey struct{}

// Middleware authenticates and replay-checks a request before invoking next.
// Failure responses always contain only the stable safe message.
func (v *Verifier) Middleware(config MiddlewareConfig, next http.Handler) (http.Handler, error) {
	if next == nil {
		return nil, fmt.Errorf("%w: next handler is required", ErrInvalidConfiguration)
	}
	status := config.FailureStatus
	if status == 0 {
		status = http.StatusUnauthorized
	}
	if status < 400 || status > 599 {
		return nil, fmt.Errorf("%w: failure status must be 4xx or 5xx", ErrInvalidConfiguration)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		verification, body, err := v.VerifyRequest(request.Context(), request, config.Request)
		if err != nil {
			invokeErrorHook(config.OnError, request.Context(), err)
			http.Error(writer, "webhook verification failed", status)

			return
		}
		ctx := context.WithValue(request.Context(), verificationContextKey{}, verification)
		ctx = context.WithValue(ctx, verifiedBodyContextKey{}, append([]byte(nil), body...))
		next.ServeHTTP(writer, request.WithContext(ctx))
	}), nil
}

func invokeErrorHook(hook ErrorHook, ctx context.Context, err error) {
	defer func() { _ = recover() }()
	if hook != nil {
		hook(ctx, err)
	}
}

// VerificationFromContext returns the authenticated verification result.
func VerificationFromContext(ctx context.Context) (Verification, bool) {
	verification, ok := ctx.Value(verificationContextKey{}).(Verification)

	return verification, ok
}

// VerifiedBodyFromContext returns a copy of the exact authenticated body.
func VerifiedBodyFromContext(ctx context.Context) ([]byte, bool) {
	body, ok := ctx.Value(verifiedBodyContextKey{}).([]byte)
	if !ok {
		return nil, false
	}

	return append([]byte(nil), body...), true
}
