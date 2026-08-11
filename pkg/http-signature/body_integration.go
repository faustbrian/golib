package httpsignature

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
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
	if transport == nil {
		return nil, ErrInvalidBodyIntegration
	}
	if request == nil {
		return nil, ErrInvalidBodyIntegration
	}
	clone, err := normalizeProtectedRequest(request)
	switch err {
	case nil:
	default:
		closeRequestBody(request)
		return nil, err
	}
	if len(clone.Header.Values("Content-Digest")) != 0 {
		closeRequestBody(request)
		return nil, ErrExistingDigest
	}
	content, err := readBoundedAndClose(request.Context(), request.Body, transport.maxBytes)
	if err != nil {
		return nil, err
	}
	clone.Trailer, err = normalizeProtectedHeader(request.Trailer)
	if err != nil {
		return nil, err
	}
	digests, err := ComputeDigests(transport.algorithms, content)
	if err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	clone.Header.Set("Content-Digest", digests.String())
	clone.Body = io.NopCloser(bytes.NewReader(content))
	deleteHeaderField(clone.Header, "Content-Length")
	deleteHeaderField(clone.Header, "Transfer-Encoding")
	deleteHeaderField(clone.Header, "Trailer")
	if len(clone.Trailer) == 0 {
		clone.ContentLength = int64(len(content))
		clone.TransferEncoding = nil
	} else {
		clone.ContentLength = -1
		clone.TransferEncoding = []string{"chunked"}
	}
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
			verifiedRequest, err := normalizeProtectedRequest(request)
			if err != nil {
				closeRequestBody(request)
				config.MapError(writer, request, err)
				return
			}
			field, err := ParseDigestFields(verifiedRequest.Header.Values("Content-Digest"))
			if err != nil {
				closeRequestBody(request)
				config.MapError(writer, request, ErrInvalidDigestField)
				return
			}
			content, err := readBoundedAndClose(request.Context(), verifiedRequest.Body, config.MaxBytes)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			verifiedRequest.Trailer, err = normalizeProtectedHeader(request.Trailer)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			if err := field.Verify(content, algorithms); err != nil {
				config.MapError(writer, request, err)
				return
			}
			clone := verifiedRequest.Clone(request.Context())
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
		if uint(count) > uint(len(buffer)) {
			return nil, ErrBodyRead
		}
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
// A response is released only after EOF finalization succeeds.
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
		config.MaxBytes <= 0 || config.Options == nil || !signingProfileCoversTrailerDigest(config.Signer.profile) ||
		signingProfileCoversProtocolDependentTrailerField(config.Signer.profile) {
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

// RoundTrip implements http.RoundTripper. The wrapped transport owns the body
// while active; an early successful response is closed and rejected, and the
// adapter closes the unfinished request body.
func (transport *TrailerSigningRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || request == nil || request.Body == nil {
		closeRequestBody(request)
		return nil, ErrInvalidBodyIntegration
	}
	if request.Method == http.MethodConnect {
		closeRequestBody(request)
		return nil, ErrInvalidBodyIntegration
	}
	clone, err := normalizeProtectedRequest(request)
	if err != nil {
		closeRequestBody(request)
		return nil, err
	}
	clone.Trailer, err = normalizeTrailerFields(clone.Trailer)
	if err != nil {
		closeRequestBody(request)
		return nil, err
	}
	if hasSignatureOrDigestFields(clone.Header) || hasSignatureOrDigestFields(clone.Trailer) {
		closeRequestBody(request)
		return nil, ErrExistingSignatures
	}
	declaredApplicationTrailers := applicationTrailerNames(clone.Trailer)

	clone.Trailer["Content-Digest"] = nil
	clone.Trailer["Signature-Input"] = nil
	clone.Trailer["Signature"] = nil
	clone.ContentLength = -1
	clone.TransferEncoding = []string{"chunked"}
	deleteHeaderField(clone.Header, "Content-Length")
	deleteHeaderField(clone.Header, "Transfer-Encoding")
	deleteHeaderField(clone.Header, "Trailer")
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
	signingBody := &trailerSigningBody{
		body: request.Body, ctx: request.Context(), maxBytes: transport.maxBytes, writers: writers,
		finalize: func(digests DigestField) error {
			trailers, err := normalizeTrailerFields(request.Trailer)
			if err != nil {
				return err
			}
			if hasSignatureOrDigestFields(trailers) {
				return ErrExistingSignatures
			}
			if !sameTrailerNames(declaredApplicationTrailers, applicationTrailerNames(trailers)) {
				return ErrInvalidBodyIntegration
			}
			clearHTTPHeader(clone.Trailer)
			for name, values := range trailers {
				clone.Trailer[name] = append([]string(nil), values...)
			}
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
	clone.Body = signingBody
	completion := signingBody.completion()
	response, roundTripErr := transport.transport.RoundTrip(clone)
	if roundTripErr != nil {
		return response, roundTripErr
	}
	if response == nil {
		_ = signingBody.Close()
		return nil, ErrInvalidBodyIntegration
	}
	select {
	case <-completion:
		if completionErr := signingBody.completionError(); completionErr != nil {
			closeResponseBody(response)
			return nil, completionErr
		}
		return response, nil
	default:
		_ = signingBody.Close()
		closeResponseBody(response)
		return nil, ErrBodyRead
	}
}

func closeResponseBody(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

type digestWriter struct {
	algorithm DigestAlgorithm
	hash      hash.Hash
}

type trailerSigningBody struct {
	body        io.ReadCloser
	ctx         context.Context
	maxBytes    int64
	written     int64
	writers     []digestWriter
	finalize    func(DigestField) error
	mu          sync.Mutex
	done        chan struct{}
	terminal    bool
	eofObserved bool
	result      error
	closeOnce   sync.Once
	closeErr    error
}

func (body *trailerSigningBody) Read(buffer []byte) (int, error) {
	body.mu.Lock()
	if body.terminal {
		result := body.result
		body.mu.Unlock()
		if result != nil {
			return 0, result
		}
		return 0, io.EOF
	}
	body.mu.Unlock()
	switch err := body.ctx.Err(); err {
	case nil:
	default:
		body.complete(err)
		return 0, err
	}
	count, err := body.body.Read(buffer)
	if count < 0 || count > len(buffer) {
		body.complete(ErrBodyRead)
		return 0, ErrBodyRead
	}
	body.mu.Lock()
	if body.terminal {
		result := body.result
		body.mu.Unlock()
		if result == nil {
			result = io.EOF
		}
		return 0, result
	}
	if int64(count) > body.maxBytes-body.written {
		body.completeLocked(ErrBodyTooLarge)
		body.mu.Unlock()
		return 0, ErrBodyTooLarge
	}
	if count == 0 && err == nil {
		body.completeLocked(ErrBodyRead)
		body.mu.Unlock()
		return 0, ErrBodyRead
	}
	body.written = body.written + int64(count)
	for _, writer := range body.writers {
		_, _ = writer.hash.Write(buffer[:count])
	}
	if err != nil && !errors.Is(err, io.EOF) {
		body.completeLocked(ErrBodyRead)
		body.mu.Unlock()
		return count, ErrBodyRead
	}
	if !errors.Is(err, io.EOF) {
		body.mu.Unlock()
		return count, nil
	}
	body.eofObserved = true
	entries := make([]Digest, len(body.writers))
	for index, writer := range body.writers {
		entries[index] = Digest{Algorithm: writer.algorithm, Value: writer.hash.Sum(nil)}
	}
	body.mu.Unlock()
	slices.SortFunc(entries, func(left, right Digest) int { return cmp.Compare(left.Algorithm, right.Algorithm) })
	finalizeErr := body.finalize(DigestField{entries: entries})
	body.complete(finalizeErr)
	if finalizeErr != nil {
		return count, finalizeErr
	}
	return count, io.EOF
}

func (body *trailerSigningBody) Close() error {
	body.closeOnce.Do(func() { body.closeErr = body.body.Close() })
	body.mu.Lock()
	if body.closeErr != nil || !body.eofObserved {
		body.completeLocked(ErrBodyRead)
	}
	result := body.closeErr
	body.mu.Unlock()
	if result != nil {
		return ErrBodyRead
	}
	return nil
}

func (body *trailerSigningBody) completion() <-chan struct{} {
	body.mu.Lock()
	defer body.mu.Unlock()
	if body.done == nil {
		body.done = make(chan struct{})
	}
	return body.done
}

func (body *trailerSigningBody) completionError() error {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.result
}

func (body *trailerSigningBody) complete(result error) {
	body.mu.Lock()
	defer body.mu.Unlock()
	body.completeLocked(result)
}

func (body *trailerSigningBody) completeLocked(result error) {
	if body.terminal {
		return
	}
	body.terminal = true
	body.result = result
	if body.done != nil {
		close(body.done)
	}
}

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

func signingProfileCoversProtocolDependentTrailerField(profile *SigningProfile) bool {
	for _, component := range profile.components {
		switch component.Name {
		case "connection", "content-length", "keep-alive", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade":
			return true
		}
	}
	return false
}

func hasSignatureOrDigestFields(header http.Header) bool {
	for _, name := range []string{"Content-Digest", "Signature-Input", "Signature"} {
		values, present, err := protectedFieldValues(header, name)
		if err != nil || present && len(values) != 0 {
			return true
		}
	}
	return false
}

func normalizeTrailerFields(header http.Header) (http.Header, error) {
	normalized := make(http.Header, len(header))
	for name, values := range header {
		canonical := http.CanonicalHeaderKey(name)
		if name == "" || !validHTTPToken(name) || !validResponseTrailerName(canonical) {
			return nil, ErrInvalidBodyIntegration
		}
		if _, duplicate := normalized[canonical]; duplicate {
			return nil, ErrAmbiguousProtectedField
		}
		normalized[canonical] = append([]string(nil), values...)
	}
	return normalized, nil
}

func applicationTrailerNames(header http.Header) map[string]struct{} {
	names := make(map[string]struct{}, len(header))
	for name := range header {
		canonical := http.CanonicalHeaderKey(name)
		if canonical != "Content-Digest" && canonical != "Signature-Input" && canonical != "Signature" {
			names[canonical] = struct{}{}
		}
	}
	return names
}

func sameTrailerNames(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if _, ok := right[name]; !ok {
			return false
		}
	}
	return true
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
		config.Options == nil || config.ReportError == nil || !signingProfileCoversTrailerDigest(config.Signer.profile) ||
		signingProfileCoversProtocolDependentTrailerField(config.Signer.profile) {
		return nil, ErrInvalidBodyIntegration
	}
	if _, err := ComputeDigests(config.Algorithms, nil); err != nil {
		return nil, ErrInvalidBodyIntegration
	}
	algorithms := append([]DigestAlgorithm(nil), config.Algorithms...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if next == nil || request == nil {
				rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, ErrInvalidBodyIntegration, config.ReportError)
				return
			}
			if request.Method == http.MethodHead || request.ProtoMajor == 1 && request.ProtoMinor == 0 {
				rejectUnsignedTrailerResponse(writer, request, http.StatusNotImplemented, ErrInvalidBodyIntegration, config.ReportError)
				return
			}
			normalizedHeader, normalizeErr := normalizeProtectedHeader(writer.Header())
			if normalizeErr != nil {
				rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, normalizeErr, config.ReportError)
				return
			}
			if hasSignatureOrDigestFields(normalizedHeader) {
				rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, ErrInvalidBodyIntegration, config.ReportError)
				return
			}
			relatedRequest, normalizeErr := normalizeProtectedRequest(request)
			if normalizeErr != nil {
				rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, normalizeErr, config.ReportError)
				return
			}
			options, err := config.Options(request.Context(), cloneRequestSnapshot(relatedRequest))
			if err != nil {
				rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, ErrHTTPIntegrationSigning, config.ReportError)
				return
			}
			var external *ExternalRequestContext
			if config.ExternalContext != nil {
				external, err = config.ExternalContext(request.Context(), cloneRequestSnapshot(relatedRequest))
				if err != nil {
					rejectUnsignedTrailerResponse(writer, request, http.StatusInternalServerError, ErrHTTPIntegrationSigning, config.ReportError)
					return
				}
			}

			writers := make([]digestWriter, len(algorithms))
			for index, algorithm := range algorithms {
				writerHash, _ := newDigestWriter(algorithm)
				writers[index] = digestWriter{algorithm: algorithm, hash: writerHash}
			}
			stream := &trailerResponseWriter{
				ResponseWriter: writer, request: relatedRequest, maxBytes: config.MaxBytes, writers: writers,
			}
			next.ServeHTTP(stream, request)
			if stream.status == 0 {
				if stream.failure == nil {
					stream.WriteHeader(http.StatusOK)
				}
			}
			if stream.failure != nil {
				if stream.status == 0 {
					rejectUnsignedTrailerResponse(writer, request, stream.failureStatusCode(), stream.failure, config.ReportError)
				} else {
					removeTrailerSigningFields(writer.Header())
					config.ReportError(request, stream.failure)
				}
				return
			}
			normalizedHeader, normalizeErr = normalizeProtectedHeader(writer.Header())
			if normalizeErr != nil {
				removeTrailerSigningFields(writer.Header())
				config.ReportError(request, normalizeErr)
				return
			}
			if hasSignatureOrDigestFields(normalizedHeader) {
				removeTrailerSigningFields(writer.Header())
				config.ReportError(request, ErrExistingSignatures)
				return
			}
			entries := make([]Digest, len(stream.writers))
			for index, digestWriter := range stream.writers {
				entries[index] = Digest{Algorithm: digestWriter.algorithm, Value: digestWriter.hash.Sum(nil)}
			}
			slices.SortFunc(entries, func(left, right Digest) int { return cmp.Compare(left.Algorithm, right.Algorithm) })
			digests := DigestField{entries: entries}
			trailers, trailerErr := stream.signingTrailers(digests)
			if trailerErr != nil {
				removeTrailerSigningFields(writer.Header())
				config.ReportError(request, trailerErr)
				return
			}
			response := &http.Response{
				StatusCode: stream.status, Header: stream.committedHeader.Clone(), Trailer: trailers,
				Body: http.NoBody, Request: relatedRequest,
			}
			configureTrailerSigningResponse(response, relatedRequest)
			message := MessageContext{
				Response: response, RelatedRequest: relatedRequest, ExternalRequest: external,
				ResponseTransport: ResponseTransportWrite,
			}
			signed, signErr := config.Signer.Sign(request.Context(), message, config.Label, options)
			if signErr != nil {
				removeTrailerSigningFields(writer.Header())
				config.ReportError(request, ErrHTTPIntegrationSigning)
				return
			}
			writer.Header().Set("Content-Digest", digests.String())
			writer.Header().Set("Signature-Input", signed.SignatureInputField())
			writer.Header().Set("Signature", signed.SignatureField())
		})
	}, nil
}

func configureTrailerSigningResponse(response *http.Response, relatedRequest *http.Request) {
	if relatedRequest.ProtoMajor != 1 {
		return
	}
	response.Proto = relatedRequest.Proto
	response.ProtoMajor = relatedRequest.ProtoMajor
	response.ProtoMinor = relatedRequest.ProtoMinor
	response.ContentLength = -1
	response.TransferEncoding = []string{"chunked"}
}

type trailerResponseWriter struct {
	http.ResponseWriter
	request         *http.Request
	committedHeader http.Header
	trailerNames    []string
	status          int
	maxBytes        int64
	written         int64
	writers         []digestWriter
	failure         error
	failureStatus   int
}

// Unwrap permits http.ResponseController to access supported optional
// interfaces such as flushing and deadlines. Hijack and full duplex are
// intercepted below because they cannot preserve trailer finalization.
func (writer *trailerResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// FlushError commits through the wrapper before delegating so validation,
// status tracking, and mandatory trailer framing cannot be bypassed.
func (writer *trailerResponseWriter) FlushError() error {
	if writer.failure != nil {
		return writer.failure
	}
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.failure != nil {
		return writer.failure
	}
	err := http.NewResponseController(writer.ResponseWriter).Flush()
	if err == nil || errors.Is(err, http.ErrNotSupported) {
		return err
	}
	writer.failure = ErrBodyRead
	return writer.failure
}

// Hijack rejects protocol takeover because it bypasses mandatory digest and
// signature trailer finalization.
func (writer *trailerResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.rejectProtocolBypass()
	return nil, nil, ErrInvalidBodyIntegration
}

// EnableFullDuplex rejects concurrent request reads and response writes because
// they bypass the middleware's bounded, finalizable response contract.
func (writer *trailerResponseWriter) EnableFullDuplex() error {
	writer.rejectProtocolBypass()
	return ErrInvalidBodyIntegration
}

func (writer *trailerResponseWriter) rejectProtocolBypass() {
	if writer.failure == nil {
		writer.failure = ErrInvalidBodyIntegration
	}
}

func (writer *trailerResponseWriter) WriteHeader(status int) {
	if writer.failure != nil || writer.status != 0 {
		return
	}
	if status == http.StatusSwitchingProtocols {
		writer.failure = ErrInvalidBodyIntegration
		writer.failureStatus = http.StatusNotImplemented
		return
	}
	if writer.request.Method == http.MethodConnect && status >= 200 && status <= 299 {
		writer.rejectSuccessfulConnect()
		return
	}
	if status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified {
		writer.failure = ErrInvalidBodyIntegration
		writer.failureStatus = http.StatusNotImplemented
		return
	}
	normalizedHeader, err := normalizeProtectedHeader(writer.Header())
	if err != nil {
		writer.failure = err
		return
	}
	if hasSignatureOrDigestFields(writer.Header()) {
		writer.failure = ErrExistingSignatures
		return
	}
	trailerNames, err := responseTrailerNames(writer.Header())
	if err != nil {
		writer.failure = err
		return
	}
	deleteHeaderField(writer.Header(), "Content-Length")
	deleteHeaderField(writer.Header(), "Transfer-Encoding")
	deleteHeaderField(writer.Header(), "Trailer")
	writer.Header().Set("Trailer", strings.Join(trailerNames, ","))
	deleteHeaderField(normalizedHeader, "Content-Length")
	deleteHeaderField(normalizedHeader, "Transfer-Encoding")
	deleteHeaderField(normalizedHeader, "Trailer")
	normalizedHeader.Set("Trailer", strings.Join(trailerNames, ","))
	writer.ResponseWriter.WriteHeader(status)
	if status < 100 || status > 199 || status == http.StatusSwitchingProtocols {
		writer.status = status
		writer.committedHeader = normalizedHeader
		writer.trailerNames = trailerNames
	}
}

func (writer *trailerResponseWriter) rejectSuccessfulConnect() {
	writer.failure = ErrInvalidBodyIntegration
	clearHTTPHeader(writer.Header())
	writer.ResponseWriter.WriteHeader(http.StatusNotImplemented)
	writer.status = http.StatusNotImplemented
}

func (writer *trailerResponseWriter) failureStatusCode() int {
	if writer.failureStatus != 0 {
		return writer.failureStatus
	}
	return http.StatusInternalServerError
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
	if uint(count) > uint(len(content)) {
		writer.failure = ErrBodyRead
		return 0, writer.failure
	}
	if writer.request.Method != http.MethodHead && responseBodyAllowed(writer.status) {
		writer.written = writer.written + int64(count)
		for _, digestWriter := range writer.writers {
			_, _ = digestWriter.hash.Write(content[:count])
		}
	}
	if err != nil || count != len(content) {
		writer.failure = ErrBodyRead
		return count, writer.failure
	}
	return count, nil
}

func (writer *trailerResponseWriter) signingTrailers(digests DigestField) (http.Header, error) {
	trailers := make(http.Header, len(writer.trailerNames))
	declaredNames := make(map[string]struct{}, len(writer.trailerNames))
	for _, name := range writer.trailerNames {
		values, _, err := protectedFieldValues(writer.Header(), name)
		if err != nil {
			return nil, err
		}
		trailers[name] = values
		declaredNames[name] = struct{}{}
	}
	lateNames := make(map[string]struct{})
	for key, values := range writer.Header() {
		name, late := strings.CutPrefix(key, http.TrailerPrefix)
		if !late {
			continue
		}
		canonical := http.CanonicalHeaderKey(name)
		if name == "" || !validHTTPToken(name) || !validResponseTrailerName(canonical) {
			return nil, ErrInvalidBodyIntegration
		}
		if _, declared := declaredNames[canonical]; declared {
			return nil, ErrAmbiguousProtectedField
		}
		if _, duplicate := lateNames[canonical]; duplicate {
			return nil, ErrAmbiguousProtectedField
		}
		lateNames[canonical] = struct{}{}
		trailers[canonical] = append([]string(nil), values...)
	}
	trailers.Set("Content-Digest", digests.String())
	return normalizeProtectedHeader(trailers)
}

func responseTrailerNames(header http.Header) ([]string, error) {
	values, present, err := protectedFieldValues(header, "Trailer")
	if err != nil {
		return nil, err
	}
	names := map[string]struct{}{
		"Content-Digest":  {},
		"Signature":       {},
		"Signature-Input": {},
	}
	if present {
		for _, value := range values {
			for _, rawName := range strings.Split(value, ",") {
				name := strings.Trim(rawName, " \t")
				canonical := http.CanonicalHeaderKey(name)
				if name == "" || !validHTTPToken(name) || !validResponseTrailerName(canonical) {
					return nil, ErrInvalidBodyIntegration
				}
				names[canonical] = struct{}{}
			}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)
	return ordered, nil
}

func validResponseTrailerName(name string) bool {
	if strings.HasPrefix(name, "If-") {
		return false
	}
	switch name {
	case "Authorization", "Cache-Control", "Connection", "Content-Encoding", "Content-Length", "Content-Range", "Content-Type",
		"Expect", "Host", "Keep-Alive", "Max-Forwards", "Pragma", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Range", "Realm", "Te", "Trailer", "Transfer-Encoding", "Www-Authenticate":
		return false
	default:
		return true
	}
}

func rejectUnsignedTrailerResponse(
	writer http.ResponseWriter,
	request *http.Request,
	status int,
	err error,
	report func(*http.Request, error),
) {
	clearHTTPHeader(writer.Header())
	writer.WriteHeader(status)
	report(request, err)
}

func clearHTTPHeader(header http.Header) {
	for name := range header {
		delete(header, name)
	}
}

func removeTrailerSigningFields(header http.Header) {
	for _, name := range []string{"Content-Digest", "Signature-Input", "Signature", "Trailer"} {
		deleteHeaderField(header, name)
	}
	for name := range header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			delete(header, name)
		}
	}
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
			verifiedRequest, err := normalizeProtectedRequest(request)
			if err != nil {
				closeRequestBody(request)
				config.MapError(writer, request, err)
				return
			}
			content, err := readBoundedAndClose(request.Context(), verifiedRequest.Body, config.MaxBytes)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			verifiedRequest.Trailer, err = normalizeProtectedHeader(request.Trailer)
			if err != nil {
				config.MapError(writer, request, err)
				return
			}
			digestField, err := ParseDigestFields(verifiedRequest.Trailer.Values("Content-Digest"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidDigestField)
				return
			}
			if err := digestField.Verify(content, algorithms); err != nil {
				config.MapError(writer, request, err)
				return
			}
			inputs, err := ParseSignatureInputs(verifiedRequest.Trailer.Values("Signature-Input"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidSignatureInput)
				return
			}
			signatures, err := ParseSignatures(verifiedRequest.Trailer.Values("Signature"))
			if err != nil {
				config.MapError(writer, request, ErrInvalidSignature)
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
			clone := verifiedRequest.Clone(context.WithValue(request.Context(), verifiedSignatureContextKey{}, verified))
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
	relatedRequest, err := snapshotResponseRequest(response.Request)
	if err != nil {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return fail(ErrInvalidHTTPIntegration)
	}
	response.Header, err = normalizeProtectedHeader(response.Header)
	if err != nil {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return fail(err)
	}
	if responseTransitionsProtocol(relatedRequest, response) {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return fail(ErrInvalidHTTPIntegration)
	}
	if response.Uncompressed {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return fail(ErrInvalidBodyIntegration)
	}
	content, err := readBoundedAndClose(request.Context(), response.Body, transport.maxBytes)
	if err != nil {
		return fail(err)
	}
	response.Trailer, err = normalizeProtectedHeader(response.Trailer)
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
	label, err := transport.selectLabel(
		cloneRequestSnapshot(relatedRequest), responseCallbackSnapshot(response, relatedRequest), inputs, signatures,
	)
	if err != nil || !validSignatureLabel(label) {
		return fail(verificationError(VerificationSelection, ErrInvalidHTTPIntegration))
	}
	message := MessageContext{
		Response: response, RelatedRequest: relatedRequest, ResponseTransport: ResponseTransportReceived,
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
	response.Body = io.NopCloser(bytes.NewReader(content))
	response.ContentLength = int64(len(content))
	response.Request = relatedRequest.WithContext(context.WithValue(relatedRequest.Context(), verifiedSignatureContextKey{}, verified))
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
