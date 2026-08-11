package httpsignature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	// ErrAmbiguousProtectedField reports case-colliding map keys for a protected
	// HTTP field whose values cannot be interpreted unambiguously.
	ErrAmbiguousProtectedField = errors.New("http signature: ambiguous protected HTTP field")
)

var protectedHTTPFieldNames = [...]string{"Signature-Input", "Signature", "Accept-Signature", "Content-Digest"}

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
	clone, err := normalizeProtectedRequest(request)
	if err != nil {
		return signingRoundTripError(request, err)
	}
	inputValues := clone.Header.Values("Signature-Input")
	signatureValues := clone.Header.Values("Signature")
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
			verifiedRequest, err := normalizeProtectedRequest(request)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			inputs, err := ParseSignatureInputs(verifiedRequest.Header.Values("Signature-Input"))
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			signatures, err := ParseSignatures(verifiedRequest.Header.Values("Signature"))
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			label, err := config.SelectLabel(cloneRequestSnapshot(verifiedRequest), inputs, signatures)
			if err != nil || !validSignatureLabel(label) {
				config.MapError(writer, request, ErrInvalidHTTPIntegration)
				return
			}
			message := MessageContext{Request: verifiedRequest}
			if config.ExternalContext != nil {
				external, externalErr := config.ExternalContext(request.Context(), cloneRequestSnapshot(verifiedRequest))
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
			next.ServeHTTP(writer, verifiedRequest.WithContext(ctx))
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
// signing boundary. MaxBufferedBytes and ReportError are mandatory; the latter
// records redacted output failures after signed headers have committed.
// Handlers requiring streaming, flushing, hijacking, or full-duplex operation
// need a trailer-aware adapter instead.
type ResponseSigningMiddlewareConfig struct {
	Signer                  *Signer
	Label                   string
	Existing                ExistingSignaturesPolicy
	MaxBufferedBytes        int64
	ContentDigestAlgorithms []DigestAlgorithm
	Options                 func(context.Context, *http.Request, *http.Response) (SigningOptions, error)
	ExternalContext         func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
	MapError                func(http.ResponseWriter, *http.Request, error)
	ReportError             func(*http.Request, error)
}

// ResponseSigningMiddleware wraps an http.Handler.
type ResponseSigningMiddleware func(http.Handler) http.Handler

// NewResponseSigningMiddleware validates an explicit response-signing policy.
// Configuration, mapping, and reporting callbacks and the signing profile must
// be safe for concurrent handler calls.
func NewResponseSigningMiddleware(config ResponseSigningMiddlewareConfig) (ResponseSigningMiddleware, error) {
	if config.Signer == nil || !validSignatureLabel(config.Label) || config.MaxBufferedBytes <= 0 || config.Options == nil ||
		config.MapError == nil || config.ReportError == nil ||
		config.Existing < ExistingSignaturesReject || config.Existing > ExistingSignaturesAppend {
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
			outerHeader, normalizeErr := normalizeProtectedHeader(writer.Header())
			if normalizeErr != nil {
				config.MapError(writer, request, normalizeErr)
				return
			}
			if len(outerHeader.Values("Content-Digest")) != 0 {
				config.MapError(writer, request, ErrExistingDigest)
				return
			}
			if len(outerHeader.Values("Signature-Input")) != 0 || len(outerHeader.Values("Signature")) != 0 {
				config.MapError(writer, request, ErrExistingSignatures)
				return
			}
			for _, name := range []string{"Content-Length", "Transfer-Encoding", "Trailer"} {
				_, present, framingErr := protectedFieldValues(writer.Header(), name)
				if framingErr != nil {
					config.MapError(writer, request, framingErr)
					return
				}
				if present {
					config.MapError(writer, request, ErrInvalidHTTPIntegration)
					return
				}
			}
			relatedRequest, normalizeErr := normalizeProtectedRequest(request)
			if normalizeErr != nil {
				config.MapError(writer, request, normalizeErr)
				return
			}
			buffer := newBufferedResponseWriter(config.MaxBufferedBytes)
			buffer.header = outerHeader.Clone()
			next.ServeHTTP(buffer, request)
			if buffer.tooLarge {
				config.MapError(writer, request, ErrResponseBodyTooLarge)
				return
			}

			response := buffer.response(relatedRequest)
			if responseTransitionsProtocol(relatedRequest, response) {
				config.MapError(writer, request, ErrInvalidHTTPIntegration)
				return
			}
			normalizedHeader, normalizeErr := normalizeProtectedHeader(response.Header)
			if normalizeErr != nil {
				config.MapError(writer, request, normalizeErr)
				return
			}
			response.Header = normalizedHeader
			normalizeErr = rejectBufferedResponseManagedFraming(response.Header)
			switch normalizeErr {
			case nil:
			default:
				config.MapError(writer, request, normalizeErr)
				return
			}
			content := buffer.body.Bytes()
			switch {
			case request.Method == http.MethodHead:
				content = nil
				response.ContentLength, normalizeErr = normalizeBufferedRepresentationContentLength(response.Header, response.ContentLength)
			case response.StatusCode == http.StatusNotModified:
				content = nil
				response.ContentLength, normalizeErr = normalizeBufferedRepresentationContentLength(response.Header, 0)
			case !responseBodyAllowed(response.StatusCode):
				content = nil
				response.ContentLength = 0
				normalizeErr = rejectBufferedContentLength(response.Header)
			default:
				normalizeErr = normalizeBufferedContentLength(response.Header, response.ContentLength, true)
			}
			if normalizeErr != nil {
				config.MapError(writer, request, normalizeErr)
				return
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

			options, err := config.Options(request.Context(), cloneRequestSnapshot(relatedRequest), responseCallbackSnapshot(response, relatedRequest))
			if err != nil {
				config.MapError(writer, request, ErrHTTPIntegrationSigning)
				return
			}
			message := MessageContext{
				Response: response, RelatedRequest: relatedRequest, ResponseTransport: ResponseTransportWrite,
			}
			if config.ExternalContext != nil {
				external, externalErr := config.ExternalContext(request.Context(), cloneRequestSnapshot(relatedRequest), responseCallbackSnapshot(response, relatedRequest))
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
			if copyErr := copyResponse(writer, response, content); copyErr != nil {
				config.ReportError(request, copyErr)
			}
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
	return status >= 200 && status != http.StatusNoContent && status != http.StatusResetContent && status != http.StatusNotModified
}

func responseTransitionsProtocol(request *http.Request, response *http.Response) bool {
	if response.StatusCode == http.StatusSwitchingProtocols {
		return true
	}
	return request.Method == http.MethodConnect && response.StatusCode >= 200 && response.StatusCode <= 299
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

func copyResponse(writer http.ResponseWriter, response *http.Response, body []byte) error {
	for name := range writer.Header() {
		delete(writer.Header(), name)
	}
	for name, values := range response.Header {
		writer.Header()[name] = append([]string(nil), values...)
	}
	writer.WriteHeader(response.StatusCode)
	count, err := writer.Write(body)
	if err != nil || count != len(body) {
		return ErrBodyRead
	}
	return nil
}

func rejectBufferedResponseManagedFraming(header http.Header) error {
	for _, name := range []string{"Transfer-Encoding", "Trailer"} {
		_, present, err := protectedFieldValues(header, name)
		switch err {
		case nil:
		default:
			return err
		}
		if present {
			return ErrInvalidHTTPIntegration
		}
	}
	for name := range header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			return ErrInvalidHTTPIntegration
		}
	}
	return nil
}

func normalizeBufferedContentLength(header http.Header, contentLength int64, required bool) error {
	values, present, err := protectedFieldValues(header, "Content-Length")
	if err != nil {
		return err
	}
	expected := strconv.FormatInt(contentLength, 10)
	if present && (len(values) != 1 || values[0] != expected) {
		return ErrInvalidHTTPIntegration
	}
	if present || required {
		deleteHeaderField(header, "Content-Length")
		header.Set("Content-Length", expected)
	}
	return nil
}

func normalizeBufferedRepresentationContentLength(header http.Header, fallback int64) (int64, error) {
	values, present, err := protectedFieldValues(header, "Content-Length")
	if err != nil {
		return 0, err
	}
	if !present {
		if fallback > 0 {
			header.Set("Content-Length", strconv.FormatInt(fallback, 10))
		}
		return fallback, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, ErrInvalidHTTPIntegration
	}
	if strings.Trim(values[0], "0123456789") != "" {
		return 0, ErrInvalidHTTPIntegration
	}
	contentLength, parseErr := strconv.ParseInt(values[0], 10, 64)
	if parseErr != nil {
		return 0, ErrInvalidHTTPIntegration
	}
	deleteHeaderField(header, "Content-Length")
	header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
	return contentLength, nil
}

func rejectBufferedContentLength(header http.Header) error {
	_, present, err := protectedFieldValues(header, "Content-Length")
	if err != nil {
		return err
	}
	if present {
		return ErrInvalidHTTPIntegration
	}
	return nil
}

// VerifyingRoundTripperConfig defines response signature selection and trusted
// external request context. The wrapped transport retains request-body
// ownership; this adapter closes a response body when verification fails.
type VerifyingRoundTripperConfig struct {
	Transport               http.RoundTripper
	Verifier                *Verifier
	SelectLabel             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	ContentDigestAlgorithms []DigestAlgorithm
	MaxBufferedBytes        int64
	ExternalContext         func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// VerifyingRoundTripper verifies response signatures. A selected signature
// that covers a response Content-Digest is accepted only when an explicit
// bounded digest policy verifies and replaces the consumed body.
type VerifyingRoundTripper struct {
	transport               http.RoundTripper
	verifier                *Verifier
	selectLabel             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	contentDigestAlgorithms []DigestAlgorithm
	maxBufferedBytes        int64
	externalContext         func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// NewVerifyingRoundTripper validates an explicit response-verification adapter.
func NewVerifyingRoundTripper(config VerifyingRoundTripperConfig) (*VerifyingRoundTripper, error) {
	if config.Transport == nil || config.Verifier == nil || config.SelectLabel == nil {
		return nil, ErrInvalidHTTPIntegration
	}
	digestPolicyConfigured := len(config.ContentDigestAlgorithms) != 0 || config.MaxBufferedBytes != 0
	if digestPolicyConfigured {
		if config.MaxBufferedBytes <= 0 {
			return nil, ErrInvalidHTTPIntegration
		}
		if _, err := ComputeDigests(config.ContentDigestAlgorithms, nil); err != nil {
			return nil, ErrInvalidHTTPIntegration
		}
	}
	return &VerifyingRoundTripper{
		transport: config.Transport, verifier: config.Verifier, selectLabel: config.SelectLabel,
		contentDigestAlgorithms: append([]DigestAlgorithm(nil), config.ContentDigestAlgorithms...),
		maxBufferedBytes:        config.MaxBufferedBytes, externalContext: config.ExternalContext,
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
	relatedRequest, err := snapshotResponseRequest(response.Request)
	if err != nil {
		return fail(verificationError(VerificationBase, ErrInvalidHTTPIntegration))
	}
	normalizedHeader, err := normalizeProtectedHeader(response.Header)
	if err != nil {
		return fail(verificationError(VerificationBase, err))
	}
	_, err = normalizeProtectedHeader(response.Trailer)
	if err != nil {
		return fail(verificationError(VerificationBase, err))
	}
	response.Header = normalizedHeader
	inputs, err := ParseSignatureInputs(response.Header.Values("Signature-Input"))
	if err != nil {
		return fail(verificationError(VerificationSelection, ErrInvalidSignatureInput))
	}
	signatures, err := ParseSignatures(response.Header.Values("Signature"))
	if err != nil {
		return fail(verificationError(VerificationSelection, ErrInvalidSignature))
	}
	label, err := transport.selectLabel(cloneRequestSnapshot(relatedRequest), responseCallbackSnapshot(response, relatedRequest), inputs, signatures)
	if err != nil || !validSignatureLabel(label) {
		return fail(verificationError(VerificationSelection, ErrInvalidHTTPIntegration))
	}
	selectedInput, _, ok := selectSignature(label, inputs, signatures)
	if !ok {
		return fail(verificationError(VerificationSelection, ErrInvalidSignedFields))
	}
	verifiesContent, authenticatesDigestPolicy := signatureInputAuthenticatesResponseContentDigest(
		selectedInput, transport.contentDigestAlgorithms,
	)
	if !responseDigestBufferingConfigured(verifiesContent, transport.contentDigestAlgorithms, transport.maxBufferedBytes) {
		return fail(verificationError(VerificationPolicy, ErrInvalidHTTPIntegration))
	}
	if verifiesContent && !authenticatesDigestPolicy {
		return fail(verificationError(VerificationPolicy, ErrInvalidHTTPIntegration))
	}
	if verifiesContent && responseTransitionsProtocol(relatedRequest, response) {
		return fail(verificationError(VerificationPolicy, ErrInvalidHTTPIntegration))
	}
	if verifiesContent && response.Uncompressed {
		return fail(verificationError(VerificationBase, ErrInvalidBodyIntegration))
	}
	wireContentLength := response.ContentLength
	if verifiesContent {
		digestField, parseErr := ParseDigestFields(response.Header.Values("Content-Digest"))
		if parseErr != nil {
			return fail(verificationError(VerificationBase, ErrInvalidDigestField))
		}
		receivedBody := response.Body
		response.Body = nil
		content, readErr := readBoundedAndClose(request.Context(), receivedBody, transport.maxBufferedBytes)
		if readErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrHTTPIntegrationVerification, readErr)
		}
		if digestErr := digestField.Verify(content, transport.contentDigestAlgorithms); digestErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrHTTPIntegrationVerification, digestErr)
		}
		response.Trailer, readErr = normalizeProtectedHeader(response.Trailer)
		if readErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrHTTPIntegrationVerification, readErr)
		}
		response.Body = io.NopCloser(bytes.NewReader(content))
		response.ContentLength = int64(len(content))
	}
	messageResponse := response
	if verifiesContent {
		messageResponse = responseCallbackSnapshot(response, relatedRequest)
		messageResponse.ContentLength = wireContentLength
	}
	message := MessageContext{
		Response: messageResponse, RelatedRequest: relatedRequest, ResponseTransport: ResponseTransportReceived,
	}
	if transport.externalContext != nil {
		external, externalErr := transport.externalContext(
			request.Context(), cloneRequestSnapshot(relatedRequest), responseCallbackSnapshot(response, relatedRequest),
		)
		if externalErr != nil {
			return fail(verificationError(VerificationBase, ErrInvalidHTTPIntegration))
		}
		message.ExternalRequest = external
	}
	verified, err := transport.verifier.Verify(request.Context(), message, label, inputs, signatures)
	if err != nil {
		return fail(err)
	}
	response.Request = relatedRequest.WithContext(context.WithValue(relatedRequest.Context(), verifiedSignatureContextKey{}, verified))
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

func snapshotResponseRequest(request *http.Request) (*http.Request, error) {
	if request == nil || request.URL == nil {
		return nil, ErrInvalidHTTPIntegration
	}
	return normalizeProtectedRequest(request)
}

func cloneRequestSnapshot(request *http.Request) *http.Request {
	clone := request.Clone(request.Context())
	clone.Body = nil
	clone.GetBody = nil
	clone.Response = nil
	clone.TLS = nil
	return clone
}

func responseCallbackSnapshot(response *http.Response, request *http.Request) *http.Response {
	clone := *response
	clone.Header = response.Header.Clone()
	clone.Trailer = response.Trailer.Clone()
	clone.TransferEncoding = append([]string(nil), response.TransferEncoding...)
	clone.Body = nil
	clone.Request = cloneRequestSnapshot(request)
	clone.TLS = nil
	return &clone
}

func normalizeProtectedRequest(request *http.Request) (*http.Request, error) {
	clone := request.Clone(request.Context())
	var err error
	clone.Header, err = normalizeProtectedHeader(request.Header)
	if err != nil {
		return nil, err
	}
	clone.Trailer, err = normalizeProtectedHeader(request.Trailer)
	if err != nil {
		return nil, err
	}
	return clone, nil
}

func normalizeProtectedHeader(header http.Header) (http.Header, error) {
	normalized := header.Clone()
	if normalized == nil {
		normalized = make(http.Header)
	}
	for _, name := range protectedHTTPFieldNames {
		values, present, err := protectedFieldValues(header, name)
		if err != nil {
			return nil, err
		}
		for key := range normalized {
			if strings.EqualFold(key, name) {
				delete(normalized, key)
			}
		}
		if present {
			normalized[http.CanonicalHeaderKey(name)] = values
		}
	}
	return normalized, nil
}

func protectedFieldValues(header http.Header, name string) ([]string, bool, error) {
	var values []string
	present := false
	for key, current := range header {
		if !strings.EqualFold(key, name) {
			continue
		}
		if present {
			return nil, false, ErrAmbiguousProtectedField
		}
		present = true
		values = append([]string(nil), current...)
	}
	return values, present, nil
}

func deleteHeaderField(header http.Header, name string) {
	for key := range header {
		if strings.EqualFold(key, name) {
			delete(header, key)
		}
	}
}

func signatureInputCoversResponseContentDigest(input SignatureInput) bool {
	covered, _, _ := responseContentDigestCoverage(input)
	return covered
}

func signatureInputAuthenticatesResponseContentDigest(input SignatureInput, algorithms []DigestAlgorithm) (bool, bool) {
	covered, wholeField, keyedAlgorithms := responseContentDigestCoverage(input)
	if !covered {
		return false, false
	}
	if wholeField {
		return true, true
	}
	for _, algorithm := range algorithms {
		if _, ok := keyedAlgorithms[algorithm]; !ok {
			return true, false
		}
	}
	return true, true
}

func responseDigestBufferingConfigured(verifiesContent bool, algorithms []DigestAlgorithm, maxBufferedBytes int64) bool {
	if !verifiesContent {
		return true
	}
	return len(algorithms) != 0 && maxBufferedBytes > 0
}

func responseContentDigestCoverage(input SignatureInput) (bool, bool, map[DigestAlgorithm]struct{}) {
	covered := false
	wholeField := false
	keyedAlgorithms := make(map[DigestAlgorithm]struct{})
	for _, component := range input.Components {
		if component.Name != "content-digest" || componentParameterTrue(component, "req") || componentParameterTrue(component, "tr") {
			continue
		}
		covered = true
		key, keyed := component.Parameter("key")
		if !keyed {
			wholeField = true
		} else if algorithm, ok := key.(string); ok {
			keyedAlgorithms[DigestAlgorithm(algorithm)] = struct{}{}
		}
	}
	return covered, wholeField, keyedAlgorithms
}

func componentParameterTrue(component ComponentIdentifier, name string) bool {
	for _, parameter := range component.Parameters {
		if parameter.Name == name {
			value, ok := parameter.Value.(bool)
			return ok && value
		}
	}
	return false
}
