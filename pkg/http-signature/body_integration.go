package httpsignature

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"slices"
)

var (
	// ErrInvalidBodyIntegration reports incomplete buffered digest policy.
	ErrInvalidBodyIntegration = errors.New("http signature: invalid body digest integration")
	// ErrBodyTooLarge reports that content exceeded its explicit buffering limit.
	ErrBodyTooLarge = errors.New("http signature: body exceeds digest buffer")
	// ErrBodyRead reports a body read or closure failure without retaining
	// potentially sensitive backend details.
	ErrBodyRead = errors.New("http signature: body unavailable for digest")
	// ErrExistingDigest reports a caller-supplied Content-Digest field that this
	// fail-closed adapter refuses to replace.
	ErrExistingDigest = errors.New("http signature: existing Content-Digest rejected")
)

// BufferedContentDigestRoundTripperConfig defines eager Content-Digest
// generation. It hashes the HTTP content bytes presented to the transport,
// after any application-managed content coding.
type BufferedContentDigestRoundTripperConfig struct {
	Transport  http.RoundTripper
	Algorithms []DigestAlgorithm
	MaxBytes   int64
}

// BufferedContentDigestRoundTripper consumes and closes the caller request
// body, then delegates a cloned request with a replayable buffered body. It is
// intended to wrap a SigningRoundTripper when Content-Digest must be covered:
// digest transport outside, signing transport inside, network transport last.
type BufferedContentDigestRoundTripper struct {
	transport  http.RoundTripper
	algorithms []DigestAlgorithm
	maxBytes   int64
}

// NewBufferedContentDigestRoundTripper validates explicit algorithms and a
// positive resource limit.
func NewBufferedContentDigestRoundTripper(config BufferedContentDigestRoundTripperConfig) (*BufferedContentDigestRoundTripper, error) {
	if config.Transport == nil || config.MaxBytes <= 0 {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.Algorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	return &BufferedContentDigestRoundTripper{
		transport: config.Transport, algorithms: append([]DigestAlgorithm(nil), config.Algorithms...), maxBytes: config.MaxBytes,
	}, nil
}

// RoundTrip implements http.RoundTripper with fail-closed body ownership.
func (transport *BufferedContentDigestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil {
		return nil, ErrInvalidBodyIntegration
	}
	if len(request.Header.Values("Content-Digest")) != 0 {
		closeRequestBody(request)
		return nil, ErrExistingDigest
	}
	content, err := readBoundedAndClose(request.Context(), request.Body, transport.maxBytes)
	if err != nil {
		return nil, err
	}
	digests, err := ComputeDigests(transport.algorithms, content)
	if err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	clone := request.Clone(request.Context())
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	clone.Header.Set("Content-Digest", digests.String())
	clone.Body = io.NopCloser(bytes.NewReader(content))
	clone.ContentLength = int64(len(content))
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	}
	return transport.transport.RoundTrip(clone)
}

// BufferedContentDigestVerificationMiddlewareConfig defines eager inbound
// Content-Digest verification. MapError owns application-specific HTTP status
// and disclosure behavior.
type BufferedContentDigestVerificationMiddlewareConfig struct {
	RequiredAlgorithms []DigestAlgorithm
	MaxBytes           int64
	MapError           func(http.ResponseWriter, *http.Request, error)
}

// BufferedContentDigestVerificationMiddleware wraps an http.Handler.
type BufferedContentDigestVerificationMiddleware func(http.Handler) http.Handler

// NewBufferedContentDigestVerificationMiddleware validates an explicit,
// bounded inbound digest policy.
func NewBufferedContentDigestVerificationMiddleware(config BufferedContentDigestVerificationMiddlewareConfig) (BufferedContentDigestVerificationMiddleware, error) {
	if config.MaxBytes <= 0 || config.MapError == nil {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.RequiredAlgorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	algorithms := append([]DigestAlgorithm(nil), config.RequiredAlgorithms...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				config.MapError(writer, request, ErrInvalidBodyIntegration)
				return
			}
			field, err := ParseDigestFields(request.Header.Values("Content-Digest"))
			if err != nil {
				closeRequestBody(request)
				config.MapError(writer, request, ErrInvalidDigestField)
				return
			}
			content, err := readBoundedAndClose(request.Context(), request.Body, config.MaxBytes)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			if err := field.Verify(content, algorithms); err != nil {
				config.MapError(writer, request, err)
				return
			}
			clone := request.Clone(request.Context())
			clone.Body = io.NopCloser(bytes.NewReader(content))
			clone.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(content)), nil
			}
			next.ServeHTTP(writer, clone)
		})
	}, nil
}

func readBoundedAndClose(ctx context.Context, body io.ReadCloser, maxBytes int64) (result []byte, resultErr error) {
	if body == nil {
		return []byte{}, nil
	}
	defer func() {
		if err := body.Close(); err != nil && resultErr == nil {
			result = nil
			resultErr = ErrBodyRead
		}
	}()
	if ctx == nil || maxBytes <= 0 {
		return nil, ErrInvalidBodyIntegration
	}

	var content bytes.Buffer
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := maxBytes - int64(content.Len())
		count, err := body.Read(buffer)
		if int64(count) > remaining {
			return nil, ErrBodyTooLarge
		}
		_, _ = content.Write(buffer[:count])
		if err != nil {
			if errors.Is(err, io.EOF) {
				return content.Bytes(), nil
			}
			return nil, fmt.Errorf("%w", ErrBodyRead)
		}
		if count == 0 {
			return nil, ErrBodyRead
		}
	}
}

func closeRequestBody(request *http.Request) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
}

// TrailerSigningRoundTripperConfig defines a streaming request adapter that
// emits Content-Digest, Signature-Input, and Signature in trailers after EOF.
// The signing profile must cover content-digest with the tr parameter.
type TrailerSigningRoundTripperConfig struct {
	Transport       http.RoundTripper
	Signer          *Signer
	Label           string
	Algorithms      []DigestAlgorithm
	MaxBytes        int64
	Options         func(context.Context, *http.Request) (SigningOptions, error)
	ExternalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
}

// TrailerSigningRoundTripper streams one non-replayable request attempt. It
// does not pre-read the body. Size, hash, key, or signing failure during Read
// aborts the attempt; bytes returned by earlier reads may already be on wire.
type TrailerSigningRoundTripper struct {
	transport       http.RoundTripper
	signer          *Signer
	label           string
	algorithms      []DigestAlgorithm
	maxBytes        int64
	options         func(context.Context, *http.Request) (SigningOptions, error)
	externalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
}

// NewTrailerSigningRoundTripper validates a streaming trailer policy.
func NewTrailerSigningRoundTripper(config TrailerSigningRoundTripperConfig) (*TrailerSigningRoundTripper, error) {
	if config.Transport == nil || config.Signer == nil || config.Signer.profile == nil || !validSignatureLabel(config.Label) ||
		config.MaxBytes <= 0 || config.Options == nil || !signingProfileCoversTrailerDigest(config.Signer.profile) {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.Algorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	return &TrailerSigningRoundTripper{
		transport: config.Transport, signer: config.Signer, label: config.Label,
		algorithms: append([]DigestAlgorithm(nil), config.Algorithms...), maxBytes: config.MaxBytes,
		options: config.Options, externalContext: config.ExternalContext,
	}, nil
}

// RoundTrip implements http.RoundTripper and delegates body closure to the
// wrapped transport as required by net/http.
func (transport *TrailerSigningRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil || request.Body == nil {
		closeRequestBody(request)
		return nil, ErrInvalidBodyIntegration
	}
	if hasSignatureOrDigestFields(request.Header) || hasSignatureOrDigestFields(request.Trailer) {
		closeRequestBody(request)
		return nil, ErrExistingSignatures
	}

	clone := request.Clone(request.Context())
	if clone.Trailer == nil {
		clone.Trailer = make(http.Header)
	}
	clone.Trailer["Content-Digest"] = nil
	clone.Trailer["Signature-Input"] = nil
	clone.Trailer["Signature"] = nil
	clone.GetBody = nil

	writers := make([]digestWriter, len(transport.algorithms))
	for index, algorithm := range transport.algorithms {
		writer, err := newDigestWriter(algorithm)
		if err != nil {
			closeRequestBody(request)
			return nil, ErrInvalidBodyIntegration
		}
		writers[index] = digestWriter{algorithm: algorithm, hash: writer}
	}
	clone.Body = &trailerSigningBody{
		body: request.Body, ctx: request.Context(), maxBytes: transport.maxBytes, writers: writers,
		finalize: func(digests DigestField) error {
			clone.Trailer.Set("Content-Digest", digests.String())
			options, err := transport.options(request.Context(), request)
			if err != nil {
				return ErrHTTPIntegrationSigning
			}
			message := MessageContext{Request: clone}
			if transport.externalContext != nil {
				external, externalErr := transport.externalContext(request.Context(), request)
				if externalErr != nil {
					return ErrHTTPIntegrationSigning
				}
				message.ExternalRequest = external
			}
			signed, err := transport.signer.Sign(request.Context(), message, transport.label, options)
			if err != nil {
				return ErrHTTPIntegrationSigning
			}
			clone.Trailer.Set("Signature-Input", signed.SignatureInputField())
			clone.Trailer.Set("Signature", signed.SignatureField())
			return nil
		},
	}
	return transport.transport.RoundTrip(clone)
}

type digestWriter struct {
	algorithm DigestAlgorithm
	hash      hash.Hash
}

type trailerSigningBody struct {
	body     io.ReadCloser
	ctx      context.Context
	maxBytes int64
	written  int64
	writers  []digestWriter
	finalize func(DigestField) error
	finished bool
}

func (body *trailerSigningBody) Read(buffer []byte) (int, error) {
	if body.finished {
		return 0, io.EOF
	}
	switch err := body.ctx.Err(); err {
	case nil:
	default:
		return 0, err
	}
	count, err := body.body.Read(buffer)
	if int64(count) > body.maxBytes-body.written {
		return 0, ErrBodyTooLarge
	}
	if count == 0 && err == nil {
		return 0, ErrBodyRead
	}
	body.written = body.written + int64(count)
	for _, writer := range body.writers {
		_, _ = writer.hash.Write(buffer[:count])
	}
	if errors.Is(err, io.EOF) {
		body.finished = true
		entries := make([]Digest, len(body.writers))
		for index, writer := range body.writers {
			entries[index] = Digest{Algorithm: writer.algorithm, Value: writer.hash.Sum(nil)}
		}
		slices.SortFunc(entries, func(left, right Digest) int { return cmp.Compare(left.Algorithm, right.Algorithm) })
		if finalizeErr := body.finalize(DigestField{entries: entries}); finalizeErr != nil {
			return count, finalizeErr
		}
	}
	return count, err
}

func (body *trailerSigningBody) Close() error { return body.body.Close() }

func newDigestWriter(algorithm DigestAlgorithm) (hash.Hash, error) {
	switch algorithm {
	case SHA256:
		return sha256.New(), nil
	case SHA512:
		return sha512.New(), nil
	default:
		return nil, ErrUnsupportedDigestAlgorithm
	}
}

func signingProfileCoversTrailerDigest(profile *SigningProfile) bool {
	want := ComponentIdentifier{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}}
	wantSerialized, _ := componentComparisonKey(want)
	for _, component := range profile.components {
		serialized, _ := componentComparisonKey(component)
		if serialized == wantSerialized {
			return true
		}
	}
	return false
}

func hasSignatureOrDigestFields(header http.Header) bool {
	return len(header.Values("Content-Digest")) != 0 || len(header.Values("Signature-Input")) != 0 || len(header.Values("Signature")) != 0
}

// TrailerResponseSigningMiddlewareConfig defines streaming response signing.
// ReportError is required because a size, handler-write, key, randomness, or
// signing failure can occur after response bytes have already been emitted.
type TrailerResponseSigningMiddlewareConfig struct {
	Signer          *Signer
	Label           string
	Algorithms      []DigestAlgorithm
	MaxBytes        int64
	Options         func(context.Context, *http.Request) (SigningOptions, error)
	ExternalContext func(context.Context, *http.Request) (*ExternalRequestContext, error)
	ReportError     func(*http.Request, error)
}

// TrailerResponseSigningMiddleware wraps an http.Handler. Recipients must wait
// for EOF and reject a response whose declared digest or signature trailers are
// absent. Configuration callbacks and ReportError must be concurrency-safe.
type TrailerResponseSigningMiddleware func(http.Handler) http.Handler

// NewTrailerResponseSigningMiddleware validates an explicit streaming policy.
// The signing profile must cover Content-Digest with the tr parameter.
func NewTrailerResponseSigningMiddleware(config TrailerResponseSigningMiddlewareConfig) (TrailerResponseSigningMiddleware, error) {
	if config.Signer == nil || config.Signer.profile == nil || !validSignatureLabel(config.Label) || config.MaxBytes <= 0 ||
		config.Options == nil || config.ReportError == nil || !signingProfileCoversTrailerDigest(config.Signer.profile) {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.Algorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	algorithms := append([]DigestAlgorithm(nil), config.Algorithms...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil || hasSignatureOrDigestFields(writer.Header()) {
				config.ReportError(request, ErrInvalidBodyIntegration)
				return
			}
			options, err := config.Options(request.Context(), request)
			if err != nil {
				config.ReportError(request, ErrHTTPIntegrationSigning)
				return
			}
			var external *ExternalRequestContext
			if config.ExternalContext != nil {
				external, err = config.ExternalContext(request.Context(), request)
				if err != nil {
					config.ReportError(request, ErrHTTPIntegrationSigning)
					return
				}
			}

			writers := make([]digestWriter, len(algorithms))
			for index, algorithm := range algorithms {
				writerHash, _ := newDigestWriter(algorithm)
				writers[index] = digestWriter{algorithm: algorithm, hash: writerHash}
			}
			writer.Header().Add("Trailer", "Content-Digest")
			writer.Header().Add("Trailer", "Signature-Input")
			writer.Header().Add("Trailer", "Signature")
			stream := &trailerResponseWriter{
				ResponseWriter: writer, request: request, maxBytes: config.MaxBytes, writers: writers,
			}
			next.ServeHTTP(stream, request)
			if stream.status == 0 {
				if stream.failure == nil {
					stream.WriteHeader(http.StatusOK)
				}
			}
			if stream.failure != nil {
				config.ReportError(request, stream.failure)
				return
			}
			entries := make([]Digest, len(stream.writers))
			for index, digestWriter := range stream.writers {
				entries[index] = Digest{Algorithm: digestWriter.algorithm, Value: digestWriter.hash.Sum(nil)}
			}
			slices.SortFunc(entries, func(left, right Digest) int { return cmp.Compare(left.Algorithm, right.Algorithm) })
			digests := DigestField{entries: entries}
			trailers := http.Header{"Content-Digest": []string{digests.String()}}
			response := &http.Response{
				StatusCode: stream.status, Header: writer.Header().Clone(), Trailer: trailers, Request: request,
			}
			message := MessageContext{Response: response, RelatedRequest: request, ExternalRequest: external}
			signed, signErr := config.Signer.Sign(request.Context(), message, config.Label, options)
			if signErr != nil {
				config.ReportError(request, ErrHTTPIntegrationSigning)
				return
			}
			writer.Header().Set("Content-Digest", digests.String())
			writer.Header().Set("Signature-Input", signed.SignatureInputField())
			writer.Header().Set("Signature", signed.SignatureField())
		})
	}, nil
}

type trailerResponseWriter struct {
	http.ResponseWriter
	request  *http.Request
	status   int
	maxBytes int64
	written  int64
	writers  []digestWriter
	failure  error
}

// Unwrap permits http.ResponseController to access supported optional
// interfaces on the underlying writer without claiming unsupported methods.
func (writer *trailerResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *trailerResponseWriter) WriteHeader(status int) {
	if writer.failure != nil || writer.status != 0 {
		return
	}
	if status == http.StatusSwitchingProtocols {
		writer.failure = ErrInvalidBodyIntegration
		return
	}
	if hasSignatureOrDigestFields(writer.Header()) {
		writer.failure = ErrExistingSignatures
		return
	}
	writer.ResponseWriter.WriteHeader(status)
	if status < 100 || status > 199 || status == http.StatusSwitchingProtocols {
		writer.status = status
	}
}

func (writer *trailerResponseWriter) Write(content []byte) (int, error) {
	if writer.failure != nil {
		return 0, writer.failure
	}
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.failure != nil {
		return 0, writer.failure
	}
	if writer.request.Method != http.MethodHead && responseBodyAllowed(writer.status) && int64(len(content)) > writer.maxBytes-writer.written {
		writer.failure = ErrBodyTooLarge
		return 0, writer.failure
	}
	count, err := writer.ResponseWriter.Write(content)
	if writer.request.Method != http.MethodHead && responseBodyAllowed(writer.status) {
		writer.written = writer.written + int64(count)
		for _, digestWriter := range writer.writers {
			_, _ = digestWriter.hash.Write(content[:count])
		}
	}
	if err != nil {
		writer.failure = ErrBodyRead
	}
	return count, err
}

// BufferedTrailerVerificationMiddlewareConfig defines eager processing for a
// request whose Content-Digest and message signature arrive in trailers.
type BufferedTrailerVerificationMiddlewareConfig struct {
	Verifier           *Verifier
	SelectLabel        func(*http.Request, SignatureInputs, Signatures) (string, error)
	RequiredAlgorithms []DigestAlgorithm
	MaxBytes           int64
	ExternalContext    func(context.Context, *http.Request) (*ExternalRequestContext, error)
	MapError           func(http.ResponseWriter, *http.Request, error)
}

// BufferedTrailerVerificationMiddleware wraps an http.Handler.
type BufferedTrailerVerificationMiddleware func(http.Handler) http.Handler

// NewBufferedTrailerVerificationMiddleware validates a bounded trailer policy.
// It requires the verification profile to cover content-digest with tr so a
// successful result authenticates the digest that was checked.
func NewBufferedTrailerVerificationMiddleware(config BufferedTrailerVerificationMiddlewareConfig) (BufferedTrailerVerificationMiddleware, error) {
	if config.Verifier == nil || config.Verifier.profile == nil || config.SelectLabel == nil || config.MaxBytes <= 0 || config.MapError == nil ||
		!verificationProfileCoversTrailerDigest(config.Verifier.profile) {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.RequiredAlgorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	algorithms := append([]DigestAlgorithm(nil), config.RequiredAlgorithms...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				config.MapError(writer, request, ErrInvalidBodyIntegration)
				return
			}
			content, err := readBoundedAndClose(request.Context(), request.Body, config.MaxBytes)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			digestField, err := ParseDigestFields(request.Trailer.Values("Content-Digest"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidDigestField)
				return
			}
			if err := digestField.Verify(content, algorithms); err != nil {
				config.MapError(writer, request, err)
				return
			}
			inputs, err := ParseSignatureInputs(request.Trailer.Values("Signature-Input"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidSignatureInput)
				return
			}
			signatures, err := ParseSignatures(request.Trailer.Values("Signature"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidSignature)
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
			clone := request.Clone(context.WithValue(request.Context(), verifiedSignatureContextKey{}, verified))
			clone.Body = io.NopCloser(bytes.NewReader(content))
			clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil }
			next.ServeHTTP(writer, clone)
		})
	}, nil
}

// BufferedTrailerVerifyingRoundTripperConfig defines eager response processing
// when Content-Digest and the message signature arrive in trailers. The body
// is not returned to the caller until its bounded digest and signature verify.
type BufferedTrailerVerifyingRoundTripperConfig struct {
	Transport          http.RoundTripper
	Verifier           *Verifier
	SelectLabel        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	RequiredAlgorithms []DigestAlgorithm
	MaxBytes           int64
	ExternalContext    func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// BufferedTrailerVerifyingRoundTripper verifies response trailers after EOF
// and replaces the consumed body with a replayable in-memory copy. It closes
// the received response body on every success and failure path.
type BufferedTrailerVerifyingRoundTripper struct {
	transport          http.RoundTripper
	verifier           *Verifier
	selectLabel        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error)
	requiredAlgorithms []DigestAlgorithm
	maxBytes           int64
	externalContext    func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error)
}

// NewBufferedTrailerVerifyingRoundTripper validates an explicit bounded
// response-trailer policy. The profile must authenticate Content-Digest with
// the tr parameter.
func NewBufferedTrailerVerifyingRoundTripper(config BufferedTrailerVerifyingRoundTripperConfig) (*BufferedTrailerVerifyingRoundTripper, error) {
	if config.Transport == nil || config.Verifier == nil || config.Verifier.profile == nil || config.SelectLabel == nil || config.MaxBytes <= 0 ||
		!verificationProfileCoversTrailerDigest(config.Verifier.profile) {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.RequiredAlgorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	return &BufferedTrailerVerifyingRoundTripper{
		transport: config.Transport, verifier: config.Verifier, selectLabel: config.SelectLabel,
		requiredAlgorithms: append([]DigestAlgorithm(nil), config.RequiredAlgorithms...), maxBytes: config.MaxBytes,
		externalContext: config.ExternalContext,
	}, nil
}

// RoundTrip implements http.RoundTripper. Trailer loss, digest mismatch, and
// signature failure close the response body and return no response.
func (transport *BufferedTrailerVerifyingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil {
		return nil, ErrInvalidBodyIntegration
	}
	response, err := transport.transport.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if response == nil {
		return nil, ErrHTTPIntegrationVerification
	}
	fail := func(cause error) (*http.Response, error) {
		return nil, fmt.Errorf("%w: %w", ErrHTTPIntegrationVerification, cause)
	}
	content, err := readBoundedAndClose(request.Context(), response.Body, transport.maxBytes)
	if err != nil {
		return fail(err)
	}
	digestField, err := ParseDigestFields(response.Trailer.Values("Content-Digest"))
	if err != nil {
		return fail(ErrInvalidDigestField)
	}
	if err := digestField.Verify(content, transport.requiredAlgorithms); err != nil {
		return fail(err)
	}
	inputs, err := ParseSignatureInputs(response.Trailer.Values("Signature-Input"))
	if err != nil {
		return fail(verificationError(VerificationSelection, ErrInvalidSignatureInput))
	}
	signatures, err := ParseSignatures(response.Trailer.Values("Signature"))
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
	response.Body = io.NopCloser(bytes.NewReader(content))
	response.ContentLength = int64(len(content))
	response.Request = request.WithContext(context.WithValue(request.Context(), verifiedSignatureContextKey{}, verified))
	return response, nil
}

func verificationProfileCoversTrailerDigest(profile *VerificationProfile) bool {
	want := ComponentIdentifier{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}}
	wantSerialized, _ := componentComparisonKey(want)
	for _, component := range profile.requiredComponents {
		serialized, _ := componentComparisonKey(component)
		if serialized == wantSerialized {
			return true
		}
	}
	return false
}
