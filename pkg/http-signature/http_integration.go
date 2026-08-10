package httpsignature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrInvalidHTTPIntegration reports incomplete adapter configuration.
	ErrInvalidHTTPIntegration = errors.New("http signature: invalid HTTP integration")
	// ErrHTTPIntegrationSigning reports request-signing failure without exposing
	// message or key material.
	ErrHTTPIntegrationSigning = errors.New("http signature: HTTP request signing failed")
	// ErrExistingSignatures reports fields forbidden by adapter policy.
	ErrExistingSignatures = errors.New("http signature: existing signature fields rejected")
	// ErrHTTPIntegrationVerification reports response-verification failure
	// without exposing message or key material.
	ErrHTTPIntegrationVerification = errors.New("http signature: HTTP response verification failed")
	// ErrResponseBodyTooLarge reports that buffered response signing reached its
	// explicit resource limit before any response bytes were emitted.
	ErrResponseBodyTooLarge = errors.New("http signature: response body exceeds signing buffer")
)

// ExistingSignaturesPolicy controls how a signing transport handles fields
// already present on a caller request. The zero value is invalid.
type ExistingSignaturesPolicy uint8

const (
	// ExistingSignaturesReject prevents accidental overwrite or signature
	// confusion.
	ExistingSignaturesReject ExistingSignaturesPolicy = iota + 1
	// ExistingSignaturesAppend parses, validates, and preserves existing label
	// order before appending the new signature.
	ExistingSignaturesAppend
)

// SigningRoundTripperConfig defines an outbound request-signing boundary.
// Transport, options, label, and existing-field policy are all explicit.
type SigningRoundTripperConfig struct {
	Transport       http.RoundTripper
	Signer          *Signer
	Label           string
	Existing        ExistingSignaturesPolicy
	Options         func(context.Context, *http.Request) (SigningOptions, error)
	ExternalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
}

// SigningRoundTripper signs a cloned request immediately before delegation. It
// does not read or replace Body; the wrapped transport retains normal body
// consumption and closure ownership.
type SigningRoundTripper struct {
	transport       http.RoundTripper
	signer          *Signer
	label           string
	existing        ExistingSignaturesPolicy
	options         func(context.Context, *http.Request) (SigningOptions, error)
	externalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
}

// NewSigningRoundTripper validates an explicit outbound adapter configuration.
func NewSigningRoundTripper(config SigningRoundTripperConfig) (*SigningRoundTripper, error) {
	if config.Transport == nil || config.Signer == nil || !validSignatureLabel(config.Label) || config.Options == nil ||
		config.Existing < ExistingSignaturesReject || config.Existing > ExistingSignaturesAppend {
		return nil, ErrInvalidHTTPIntegration
	}

	return &SigningRoundTripper{
		transport:       config.Transport,
		signer:          config.Signer,
		label:           config.Label,
		existing:        config.Existing,
		options:         config.Options,
		externalContext: config.ExternalContext,
	}, nil
}

// RoundTrip implements http.RoundTripper without mutating request headers.
func (transport *SigningRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil {
		return nil, ErrInvalidHTTPIntegration
	}
	inputValues := request.Header.Values("Signature-Input")
	signatureValues := request.Header.Values("Signature")
	hasExisting := len(inputValues) != 0 || len(signatureValues) != 0
	if hasExisting && transport.existing == ExistingSignaturesReject {
		return signingRoundTripError(request, ErrExistingSignatures)
	}
	var existingInputs SignatureInputs
	var existingSignatures Signatures
	if hasExisting {
		var err error
		existingInputs, err = ParseSignatureInputs(inputValues)
		if err != nil {
			return signingRoundTripError(request, ErrExistingSignatures)
		}
		existingSignatures, err = ParseSignatures(signatureValues)
		if err != nil || !matchingLabelSets(existingInputs, existingSignatures) {
			return signingRoundTripError(request, ErrExistingSignatures)
		}
	}

	ctx := request.Context()
	options, err := transport.options(ctx, request)
	if err != nil {
		return signingRoundTripError(request, ErrHTTPIntegrationSigning)
	}

	clone := request.Clone(ctx)
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	message := MessageContext{Request: clone}
	if transport.externalContext != nil {
		external, externalErr := transport.externalContext(ctx, request)
		if externalErr != nil {
			return signingRoundTripError(request, ErrHTTPIntegrationSigning)
		}
		message.ExternalRequest = external
	}
	signed, err := transport.signer.Sign(ctx, message, transport.label, options)
	if err != nil {
		return signingRoundTripError(request, ErrHTTPIntegrationSigning)
	}

	if hasExisting {
		combinedInputs, combinedSignatures, combineErr := appendSignedFields(existingInputs, existingSignatures, signed)
		if combineErr != nil {
			return signingRoundTripError(request, ErrExistingSignatures)
		}
		clone.Header.Set("Signature-Input", combinedInputs.String())
		clone.Header.Set("Signature", combinedSignatures.String())
	} else {
		clone.Header.Set("Signature-Input", signed.SignatureInputField())
		clone.Header.Set("Signature", signed.SignatureField())
	}

	return transport.transport.RoundTrip(clone)
}

func signingRoundTripError(request *http.Request, err error) (*http.Response, error) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
	return nil, err
}

// RequestVerificationMiddlewareConfig defines inbound selection, trusted
// external-origin reconstruction, and application-specific failure mapping.
type RequestVerificationMiddlewareConfig struct {
	Verifier        *Verifier
	SelectLabel     func(*http.Request, SignatureInputs, Signatures) (string, error)
	ExternalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
	MapError        func(http.ResponseWriter, *http.Request, error)
}

// RequestVerificationMiddleware wraps an http.Handler.
type RequestVerificationMiddleware func(http.Handler) http.Handler

// NewRequestVerificationMiddleware validates an inbound adapter. It supplies
// no status-code or disclosure defaults; MapError owns application mapping.
func NewRequestVerificationMiddleware(config RequestVerificationMiddlewareConfig) (RequestVerificationMiddleware, error) {
	if config.Verifier == nil || config.SelectLabel == nil || config.MapError == nil {
		return nil, ErrInvalidHTTPIntegration
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				config.MapError(writer, request, ErrInvalidHTTPIntegration)
				return
			}
			inputs, err := ParseSignatureInputs(request.Header.Values("Signature-Input"))
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			signatures, err := ParseSignatures(request.Header.Values("Signature"))
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			label, err := config.SelectLabel(request, inputs, signatures)
			if err != nil || !validSignatureLabel(label) {
				config.MapError(writer, request, ErrInvalidHTTPIntegration)
				return
			}
			message := MessageContext{Request: request}
			if config.ExternalContext != nil {
				external, externalErr := config.ExternalContext(request.Context(), request)
				if externalErr != nil {
					config.MapError(writer, request, ErrInvalidHTTPIntegration)
					return
				}
				message.ExternalRequest = external
			}
			verified, err := config.Verifier.Verify(request.Context(), message, label, inputs, signatures)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			ctx := context.WithValue(request.Context(), verifiedSignatureContextKey{}, verified)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}, nil
}

type verifiedSignatureContextKey struct{}

// VerifiedSignatureFromContext returns profile-conformant verification metadata
// stored by RequestVerificationMiddleware. It is not an authorization result.
func VerifiedSignatureFromContext(ctx context.Context) (VerifiedSignature, bool) {
	if ctx == nil {
		return VerifiedSignature{}, false
	}
	verified, ok := ctx.Value(verifiedSignatureContextKey{}).(VerifiedSignature)
	return verified, ok
}

// ResponseSigningMiddlewareConfig defines a fail-closed buffered response
// signing boundary. MaxBufferedBytes is mandatory; handlers requiring
// streaming, flushing, hijacking, or full-duplex operation need a trailer-aware
// adapter instead.
type ResponseSigningMiddlewareConfig struct {
	Signer                  *Signer
	Label                   string
	Existing                ExistingSignaturesPolicy
	MaxBufferedBytes        int64
	ContentDigestAlgorithms []DigestAlgorithm
	Options                 func(context.Context, *http.Request, *http.Response) (SigningOptions, error)
	ExternalContext         func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
	MapError                func(http.ResponseWriter, *http.Request, error)
}

// ResponseSigningMiddleware wraps an http.Handler.
type ResponseSigningMiddleware func(http.Handler) http.Handler

// NewResponseSigningMiddleware validates an explicit response-signing policy.
// Configuration callbacks and the signing profile must be safe for concurrent
// handler calls.
func NewResponseSigningMiddleware(config ResponseSigningMiddlewareConfig) (ResponseSigningMiddleware, error) {
	if config.Signer == nil || !validSignatureLabel(config.Label) || config.MaxBufferedBytes <= 0 || config.Options == nil ||
		config.MapError == nil || config.Existing < ExistingSignaturesReject || config.Existing > ExistingSignaturesAppend {
		return nil, ErrInvalidHTTPIntegration
	}
	digestAlgorithms := append([]DigestAlgorithm(nil), config.ContentDigestAlgorithms...)
	if len(digestAlgorithms) != 0 {
		if config.Signer.profile == nil || !signingProfileCoversComponent(config.Signer.profile, ComponentIdentifier{Name: "content-digest"}) {
			return nil, ErrInvalidHTTPIntegration
		}
		if _, err := ComputeDigests(digestAlgorithms, nil); err != nil {
			return nil, ErrInvalidHTTPIntegration
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				config.MapError(writer, request, ErrInvalidHTTPIntegration)
				return
			}
			buffer := newBufferedResponseWriter(config.MaxBufferedBytes)
			next.ServeHTTP(buffer, request)
			if buffer.tooLarge {
				config.MapError(writer, request, ErrResponseBodyTooLarge)
				return
			}

			response := buffer.response(request)
			content := buffer.body.Bytes()
			if request.Method == http.MethodHead || !responseBodyAllowed(response.StatusCode) {
				content = nil
				response.ContentLength = 0
			}
			if len(digestAlgorithms) != 0 {
				if len(response.Header.Values("Content-Digest")) != 0 {
					config.MapError(writer, request, ErrExistingDigest)
					return
				}
				digests, _ := ComputeDigests(digestAlgorithms, content)
				response.Header.Set("Content-Digest", digests.String())
			}
			inputValues := response.Header.Values("Signature-Input")
			signatureValues := response.Header.Values("Signature")
			hasExisting := len(inputValues) != 0 || len(signatureValues) != 0
			if hasExisting && config.Existing == ExistingSignaturesReject {
				config.MapError(writer, request, ErrExistingSignatures)
				return
			}
			var existingInputs SignatureInputs
			var existingSignatures Signatures
			if hasExisting {
				var err error
				existingInputs, err = ParseSignatureInputs(inputValues)
				if err != nil {
					config.MapError(writer, request, ErrExistingSignatures)
					return
				}
				existingSignatures, err = ParseSignatures(signatureValues)
				if err != nil || !matchingLabelSets(existingInputs, existingSignatures) {
					config.MapError(writer, request, ErrExistingSignatures)
					return
				}
			}

			options, err := config.Options(request.Context(), request, response)
			if err != nil {
				config.MapError(writer, request, ErrHTTPIntegrationSigning)
				return
			}
			message := MessageContext{Response: response, RelatedRequest: request}
			if config.ExternalContext != nil {
				external, externalErr := config.ExternalContext(request.Context(), request, response)
				if externalErr != nil {
					config.MapError(writer, request, ErrHTTPIntegrationSigning)
					return
				}
				message.ExternalRequest = external
			}
			signed, err := config.Signer.Sign(request.Context(), message, config.Label, options)
			if err != nil {
				config.MapError(writer, request, ErrHTTPIntegrationSigning)
				return
			}
			if hasExisting {
				combinedInputs, combinedSignatures, combineErr := appendSignedFields(existingInputs, existingSignatures, signed)
				if combineErr != nil {
					config.MapError(writer, request, ErrExistingSignatures)
					return
				}
				response.Header.Set("Signature-Input", combinedInputs.String())
				response.Header.Set("Signature", combinedSignatures.String())
			} else {
				response.Header.Set("Signature-Input", signed.SignatureInputField())
				response.Header.Set("Signature", signed.SignatureField())
			}
			copyResponse(writer, response, content)
		})
	}, nil
}

type bufferedResponseWriter struct {
	header          http.Header
	committedHeader http.Header
	body            bytes.Buffer
	status          int
	maxBytes        int64
	tooLarge        bool
}

func newBufferedResponseWriter(maxBytes int64) *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header), maxBytes: maxBytes}
}

func (writer *bufferedResponseWriter) Header() http.Header { return writer.header }

func (writer *bufferedResponseWriter) WriteHeader(status int) {
	if status < 100 || status > 999 {
		panic(fmt.Sprintf("invalid WriteHeader code %d", status))
	}
	if status >= 100 && status <= 199 && status != http.StatusSwitchingProtocols {
		return
	}
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.committedHeader = writer.header.Clone()
}

func (writer *bufferedResponseWriter) Write(content []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if !responseBodyAllowed(writer.status) {
		return 0, http.ErrBodyNotAllowed
	}
	if writer.tooLarge || int64(len(content)) > writer.maxBytes-int64(writer.body.Len()) {
		writer.tooLarge = true
		return 0, ErrResponseBodyTooLarge
	}
	return writer.body.Write(content)
}

func responseBodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

func (writer *bufferedResponseWriter) response(request *http.Request) *http.Response {
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	header := writer.committedHeader
	if header == nil {
		header = writer.header
	}
	return &http.Response{
		StatusCode:    status,
		Header:        header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(writer.body.Bytes())),
		ContentLength: int64(writer.body.Len()),
		Request:       request,
	}
}

func copyResponse(writer http.ResponseWriter, response *http.Response, body []byte) {
	for name, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(body)
}

// VerifyingRoundTripperConfig defines response signature selection and trusted
// external request context. The wrapped transport retains request-body
// ownership; this adapter closes a response body when verification fails.
type VerifyingRoundTripperConfig struct {
	Transport       http.RoundTripper
	Verifier        *Verifier
	SelectLabel     func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	ExternalContext func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// VerifyingRoundTripper verifies response signatures without reading or
// replacing response bodies.
type VerifyingRoundTripper struct {
	transport       http.RoundTripper
	verifier        *Verifier
	selectLabel     func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	externalContext func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// NewVerifyingRoundTripper validates an explicit response-verification adapter.
func NewVerifyingRoundTripper(config VerifyingRoundTripperConfig) (*VerifyingRoundTripper, error) {
	if config.Transport == nil || config.Verifier == nil || config.SelectLabel == nil {
		return nil, ErrInvalidHTTPIntegration
	}
	return &VerifyingRoundTripper{
		transport: config.Transport, verifier: config.Verifier, selectLabel: config.SelectLabel, externalContext: config.ExternalContext,
	}, nil
}

// RoundTrip implements http.RoundTripper. A successful verification is stored
// on a cloned response Request context and is not an authorization result.
func (transport *VerifyingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil {
		return nil, ErrInvalidHTTPIntegration
	}
	response, err := transport.transport.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if response == nil {
		return nil, ErrHTTPIntegrationVerification
	}
	fail := func(cause error) (*http.Response, error) {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("%w: %w", ErrHTTPIntegrationVerification, cause)
	}
	inputs, err := ParseSignatureInputs(response.Header.Values("Signature-Input"))
	if err != nil {
		return fail(verificationError(VerificationSelection, ErrInvalidSignatureInput))
	}
	signatures, err := ParseSignatures(response.Header.Values("Signature"))
	if err != nil {
		return fail(verificationError(VerificationSelection, ErrInvalidSignature))
	}
	label, err := transport.selectLabel(request, response, inputs, signatures)
	if err != nil || !validSignatureLabel(label) {
		return fail(verificationError(VerificationSelection, ErrInvalidHTTPIntegration))
	}
	message := MessageContext{Response: response, RelatedRequest: request}
	if transport.externalContext != nil {
		external, externalErr := transport.externalContext(request.Context(), request, response)
		if externalErr != nil {
			return fail(verificationError(VerificationBase, ErrInvalidHTTPIntegration))
		}
		message.ExternalRequest = external
	}
	verified, err := transport.verifier.Verify(request.Context(), message, label, inputs, signatures)
	if err != nil {
		return fail(err)
	}
	response.Request = request.WithContext(context.WithValue(request.Context(), verifiedSignatureContextKey{}, verified))
	return response, nil
}

// VerifiedSignatureFromResponse returns profile-conformant verification
// metadata stored by VerifyingRoundTripper.
func VerifiedSignatureFromResponse(response *http.Response) (VerifiedSignature, bool) {
	if response == nil || response.Request == nil {
		return VerifiedSignature{}, false
	}
	return VerifiedSignatureFromContext(response.Request.Context())
}

func appendSignedFields(inputs SignatureInputs, signatures Signatures, signed SignedFields) (SignatureInputs, Signatures, error) {
	pairs := make([]SignedFields, 0)
	signatureByLabel := make(map[string]SignatureValue, len(signatures.entries))
	for _, signature := range signatures.entries {
		signatureByLabel[signature.Label] = signature
	}
	for index := range inputs.entries {
		label := inputs.entries[index].Label
		signature, ok := signatureByLabel[label]
		if !ok {
			return SignatureInputs{}, Signatures{}, ErrInvalidSignedFields
		}
		pairs = append(pairs, SignedFields{input: inputs.entries[index], signature: signature})
	}
	pairs = append(pairs, signed)
	return CombineSignedFields(pairs...)
}

func signingProfileCoversComponent(profile *SigningProfile, want ComponentIdentifier) bool {
	wantSerialized, err := componentComparisonKey(want)
	if err != nil {
		return false
	}
	for _, component := range profile.components {
		serialized, serializeErr := componentComparisonKey(component)
		if serializeErr == nil && serialized == wantSerialized {
			return true
		}
	}
	return false
}
