// Package compatibility isolates non-RFC 9421 HTTP authentication schemes
// behind caller-supplied protocol implementations. It does not parse, sign,
// verify, or reinterpret any legacy wire format itself.
package compatibility

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Protocol identifies a compatibility protocol that is deliberately separate
// from RFC 9421 fields and parsers.
type Protocol string

const (
	// CavageDraft identifies the historical Cavage HTTP Signatures drafts.
	CavageDraft Protocol = "cavage-draft"
	// AWSSigV4 identifies AWS Signature Version 4.
	AWSSigV4 Protocol = "aws-sigv4"
	// OAuth1 identifies OAuth 1.0 request signatures.
	OAuth1 Protocol = "oauth1"
)

// Operation identifies the compatibility boundary that failed.
type Operation string

const (
	// OperationSign is outbound request signing.
	OperationSign Operation = "sign"
	// OperationVerify is inbound request verification.
	OperationVerify Operation = "verify"
)

var (
	// ErrInvalidAdapter reports incomplete or unsafe adapter configuration.
	ErrInvalidAdapter = errors.New("http signature compatibility: invalid adapter")
	// ErrSigning reports a sanitized external signing failure.
	ErrSigning = errors.New("http signature compatibility: signing failed")
	// ErrVerification reports a sanitized external verification failure.
	ErrVerification = errors.New("http signature compatibility: verification failed")
)

// ErrorReporter receives the original external implementation failure. It is
// an application-owned diagnostic boundary and must redact protocol secrets.
type ErrorReporter func(context.Context, Protocol, Operation, error)

// SigningRoundTripperConfig configures an outbound compatibility adapter.
// Sign receives an isolated request view. Protocol-specific header and trailer
// mutations are delegated, except Signature-Input, Signature, and
// Accept-Signature. Request identity mutations are discarded, and Body cannot
// be read or closed through the view. Body-derived form state, TLS, and redirect
// Response graphs are omitted because net/http clones can retain aliases.
type SigningRoundTripperConfig struct {
	Transport   http.RoundTripper
	Sign        func(context.Context, *http.Request) error
	ReportError ErrorReporter
}

// SigningRoundTripper applies one explicitly selected legacy or vendor signer
// to a cloned request before delegating it. It never invokes RFC 9421 parsers.
type SigningRoundTripper struct {
	protocol  Protocol
	transport http.RoundTripper
	sign      func(context.Context, *http.Request) error
	report    ErrorReporter
}

// NewCavageSigningRoundTripper creates an outbound Cavage-draft boundary.
func NewCavageSigningRoundTripper(config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	return newSigningRoundTripper(CavageDraft, config)
}

// NewAWSSigV4SigningRoundTripper creates an outbound AWS SigV4 boundary.
func NewAWSSigV4SigningRoundTripper(config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	return newSigningRoundTripper(AWSSigV4, config)
}

// NewOAuth1SigningRoundTripper creates an outbound OAuth 1.0 boundary.
func NewOAuth1SigningRoundTripper(config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	return newSigningRoundTripper(OAuth1, config)
}

// NewVendorSigningRoundTripper creates an outbound boundary for one explicitly
// named vendor scheme. Names use lowercase Structured Field key characters.
func NewVendorSigningRoundTripper(name string, config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	protocol, ok := vendorProtocol(name)
	if !ok {
		return nil, ErrInvalidAdapter
	}
	return newSigningRoundTripper(protocol, config)
}

func newSigningRoundTripper(protocol Protocol, config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	if config.Transport == nil || config.Sign == nil || config.ReportError == nil {
		return nil, ErrInvalidAdapter
	}
	return &SigningRoundTripper{protocol: protocol, transport: config.Transport, sign: config.Sign, report: config.ReportError}, nil
}

// Protocol returns the explicitly selected compatibility protocol.
func (adapter *SigningRoundTripper) Protocol() Protocol {
	if adapter == nil {
		return ""
	}
	return adapter.protocol
}

// RoundTrip implements http.RoundTripper. The caller retains ownership of the
// original request value; normal RoundTripper body ownership transfers to the
// configured transport when delegation begins.
func (adapter *SigningRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if adapter == nil {
		return nil, ErrInvalidAdapter
	}
	if request == nil || adapter.transport == nil {
		return nil, ErrInvalidAdapter
	}
	delegated := request.Clone(request.Context())
	callbackRequest := isolatedCallbackRequest(delegated)
	if err := adapter.sign(callbackRequest.Context(), callbackRequest); err != nil {
		closeRequestBody(request)
		adapter.report(callbackRequest.Context(), adapter.protocol, OperationSign, err)
		return nil, ErrSigning
	}
	delegated.Header = compatibleSigningFields(callbackRequest.Header, delegated.Header)
	delegated.Trailer = compatibleSigningFields(callbackRequest.Trailer, delegated.Trailer)
	return adapter.transport.RoundTrip(delegated)
}

// VerificationMiddlewareConfig configures an inbound compatibility adapter.
// Verify receives an isolated request view whose mutations cannot reach later
// middleware. It communicates verification only through its returned error.
// Body, body-derived form state, TLS, and redirect Response graphs are omitted
// from the view. Reject receives only a sanitized error; ReportError is the
// separate application diagnostic seam.
type VerificationMiddlewareConfig struct {
	Verify      func(context.Context, *http.Request) error
	ReportError ErrorReporter
	Reject      func(http.ResponseWriter, *http.Request, error)
}

// VerificationMiddleware verifies one explicitly selected non-RFC protocol
// before invoking the next net/http handler.
type VerificationMiddleware func(http.Handler) http.Handler

// NewCavageVerificationMiddleware creates an inbound Cavage-draft boundary.
func NewCavageVerificationMiddleware(config VerificationMiddlewareConfig) (VerificationMiddleware, error) {
	return newVerificationMiddleware(CavageDraft, config)
}

// NewAWSSigV4VerificationMiddleware creates an inbound AWS SigV4 boundary.
func NewAWSSigV4VerificationMiddleware(config VerificationMiddlewareConfig) (VerificationMiddleware, error) {
	return newVerificationMiddleware(AWSSigV4, config)
}

// NewOAuth1VerificationMiddleware creates an inbound OAuth 1.0 boundary.
func NewOAuth1VerificationMiddleware(config VerificationMiddlewareConfig) (VerificationMiddleware, error) {
	return newVerificationMiddleware(OAuth1, config)
}

// NewVendorVerificationMiddleware creates an inbound boundary for one
// explicitly named vendor scheme.
func NewVendorVerificationMiddleware(name string, config VerificationMiddlewareConfig) (VerificationMiddleware, error) {
	protocol, ok := vendorProtocol(name)
	if !ok {
		return nil, ErrInvalidAdapter
	}
	return newVerificationMiddleware(protocol, config)
}

func newVerificationMiddleware(protocol Protocol, config VerificationMiddlewareConfig) (VerificationMiddleware, error) {
	if config.Verify == nil || config.ReportError == nil || config.Reject == nil {
		return nil, ErrInvalidAdapter
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			callbackRequest := isolatedCallbackRequest(request)
			if err := config.Verify(callbackRequest.Context(), callbackRequest); err != nil {
				config.ReportError(request.Context(), protocol, OperationVerify, err)
				config.Reject(writer, request, ErrVerification)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}, nil
}

var errCallbackBodyAccess = errors.New("http signature compatibility: callback body access is forbidden")

type isolatedBody struct{}

func (isolatedBody) Read([]byte) (int, error) {
	return 0, errCallbackBodyAccess
}

func (isolatedBody) Close() error {
	return nil
}

func isolatedCallbackRequest(request *http.Request) *http.Request {
	clone := request.Clone(request.Context())
	clone.GetBody = nil
	clone.Form = nil
	clone.PostForm = nil
	clone.MultipartForm = nil
	clone.TLS = nil
	clone.Response = nil
	if clone.Body != nil {
		clone.Body = isolatedBody{}
	}
	return clone
}

func closeRequestBody(request *http.Request) {
	if request.Body != nil {
		_ = request.Body.Close()
	}
}

func compatibleSigningFields(callback, original http.Header) http.Header {
	fields := callback.Clone()
	for name := range fields {
		if isRFC9421Field(name) {
			delete(fields, name)
		}
	}
	for name, values := range original {
		if isRFC9421Field(name) {
			if fields == nil {
				fields = make(http.Header)
			}
			fields[name] = cloneStrings(values)
		}
	}
	return fields
}

func isRFC9421Field(name string) bool {
	return strings.EqualFold(name, "Signature-Input") ||
		strings.EqualFold(name, "Signature") ||
		strings.EqualFold(name, "Accept-Signature")
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	clone := make([]string, len(values))
	copy(clone, values)
	return clone
}

func vendorProtocol(name string) (Protocol, bool) {
	if len(name) == 0 || len(name) > 64 {
		return "", false
	}
	for index := range len(name) {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return "", false
	}
	return Protocol("vendor:" + name), true
}
