// Package httpclient provides policy and lifecycle primitives for typed
// outbound HTTP integrations while preserving the standard net/http API.
package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultConnectTimeout        = 10 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 15 * time.Second
	defaultExpectContinueTimeout = time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultMaxIdleConnsPerHost   = 10
	defaultMaxResponseHeaderSize = 1 << 20
)

var (
	// ErrClientClosed indicates that an operation used a closed Client.
	ErrClientClosed = errors.New("http client is closed")
	// ErrInvalidConfig indicates that client configuration is invalid.
	ErrInvalidConfig = errors.New("invalid http client configuration")
	// ErrNilRequest indicates that Client.Do received a nil request.
	ErrNilRequest = errors.New("http request is nil")
)

// TransportOwnership defines whether Client.Close manages a configured
// transport. Callers retain ownership by default.
type TransportOwnership uint8

const (
	// TransportBorrowed leaves the configured transport under caller ownership.
	TransportBorrowed TransportOwnership = iota
	// TransportOwned transfers idle-connection cleanup to the Client.
	TransportOwned
)

// Config configures a Client. A zero Config selects finite production-safe
// defaults and an internally owned standard transport.
type Config struct {
	// Profile selects a named versioned policy. Zero selects interactive/v1.
	Profile PolicyProfileID
	// Policy overrides profile values for every operation on this client.
	Policy PolicyOverrides
	// Timeout bounds the complete HTTP exchange. Zero selects 30 seconds.
	// When set, it is the legacy client-level operation-timeout override and
	// takes precedence over Policy.OperationTimeout.
	Timeout time.Duration
	// Transport replaces the default transport. It is borrowed unless
	// TransportOwnership is TransportOwned.
	Transport http.RoundTripper
	// TransportOwnership controls cleanup of a configured Transport.
	TransportOwnership TransportOwnership
	// Middleware contains client, endpoint, request, or one-shot registrations.
	// New resolves them into one immutable pipeline.
	Middleware []Middleware
	// Session opts into an isolated cookie jar and redirect policy. Nil disables
	// ambient cookie state.
	Session *SessionConfig
	// OperationIdentityGenerator replaces the default cryptographically random
	// 128-bit logical operation identifier generator.
	OperationIdentityGenerator IdentifierGenerator
	// Egress enables destination and dial-time address enforcement. It requires
	// the internally owned standard transport.
	Egress *EgressPolicy
	// TLS configures roots, server identity, client identity, and optional SPKI
	// pins. It requires the internally owned standard transport.
	TLS *TLSPolicy
	// Telemetry enables operation and physical-attempt observation and header
	// propagation without installing a mandatory exporter or logger.
	Telemetry *TelemetryOptions
}

// Client executes standard HTTP requests and owns their operation lifecycle.
// Callers own response bodies until they close the body or close the Client.
type Client struct {
	httpClient     *http.Client
	transport      http.RoundTripper
	ownedTransport bool
	pipeline       Pipeline
	session        *clientSession
	egress         *EgressPolicy
	profile        PolicyProfileID
	policy         PolicyOverrides

	context context.Context
	cancel  context.CancelFunc

	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
	bodies    map[*managedBody]struct{}
	closeErr  error
}

// New constructs a Client without mutating any caller-provided transport.
func New(config Config) (*Client, error) {
	if config.Timeout < 0 {
		return nil, fmt.Errorf("%w: timeout must not be negative", ErrInvalidConfig)
	}
	if config.TransportOwnership > TransportOwned {
		return nil, fmt.Errorf("%w: unknown transport ownership %d", ErrInvalidConfig, config.TransportOwnership)
	}
	if config.Egress != nil && config.Transport != nil {
		return nil, fmt.Errorf("%w: egress policy requires the standard transport", ErrInvalidConfig)
	}
	if config.TLS != nil && config.Transport != nil {
		return nil, fmt.Errorf("%w: TLS policy requires the standard transport", ErrInvalidConfig)
	}
	clientPolicy := clonePolicyOverrides(config.Policy)
	if config.Timeout > 0 {
		clientPolicy.OperationTimeout = cloneDuration(&config.Timeout)
	}
	resolvedPolicy, err := ResolvePolicy(config.Profile, clientPolicy, PolicyOverrides{})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	identityMiddleware, err := newOperationIdentityMiddleware(config.OperationIdentityGenerator)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	telemetryMiddleware, err := newTelemetryMiddleware(config.Telemetry)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	session, sessionMiddleware, err := newClientSession(config.Session)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	registrations := make([]Middleware, 0, 1+len(telemetryMiddleware)+len(config.Middleware)+len(sessionMiddleware))
	registrations = append(registrations, identityMiddleware)
	registrations = append(registrations, telemetryMiddleware...)
	registrations = append(registrations, config.Middleware...)
	registrations = append(registrations, sessionMiddleware...)
	pipeline, err := NewPipeline(registrations...)
	if err != nil {
		var closeErr error
		if session != nil {
			closeErr = session.closeJar()
		}

		return nil, errors.Join(fmt.Errorf("%w: %w", ErrInvalidConfig, err), closeErr)
	}

	timeout := resolvedPolicy.Values().OperationTimeout

	transport := config.Transport
	ownedTransport := config.TransportOwnership == TransportOwned
	if transport == nil {
		transport = defaultTransportWithPolicy(resolvedPolicy.Values(), config.Egress, config.TLS)
		ownedTransport = true
	}

	lifecycleContext, cancel := context.WithCancel(context.Background())

	client := &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			Jar:       sessionJar(session),
		},
		transport:      transport,
		ownedTransport: ownedTransport,
		pipeline:       pipeline,
		session:        session,
		egress:         config.Egress,
		profile:        resolvedPolicy.Profile(),
		policy:         clientPolicy,
		context:        lifecycleContext,
		cancel:         cancel,
		bodies:         make(map[*managedBody]struct{}),
	}
	if config.Session != nil && config.Session.LoadOnStart {
		ctx, cancelLoad := context.WithTimeout(context.Background(), session.timeout)
		loadErr := client.LoadSession(ctx)
		cancelLoad()
		if loadErr != nil {
			client.session.saveOnClose = false
			return nil, errors.Join(loadErr, client.Close())
		}
	}

	return client, nil
}

func sessionJar(session *clientSession) http.CookieJar {
	if session == nil {
		return nil
	}

	return session.jar
}

// HTTPClient returns the standard client used for requests. Configuration
// should be completed before the Client is shared between goroutines.
func (client *Client) HTTPClient() *http.Client {
	return client.httpClient
}

// Do executes request through the configured standard client. The returned
// response body must be closed by the caller. Closing Client also closes every
// response body still owned by the caller.
func (client *Client) Do(request *http.Request) (*http.Response, error) {
	return client.do(request, client.pipeline)
}

// DoWithMiddleware executes request through an immutable pipeline derived from
// the client pipeline. The supplied registrations affect only this operation.
func (client *Client) DoWithMiddleware(
	request *http.Request,
	middleware ...Middleware,
) (*http.Response, error) {
	pipeline, err := client.pipeline.With(middleware...)
	if err != nil {
		return nil, err
	}

	return client.do(request, pipeline)
}

// InspectPipeline returns independent copies of the configured resolved plans.
func (client *Client) InspectPipeline() PipelineInspection {
	return client.pipeline.Inspect()
}

// InspectPolicy resolves the immutable operation policy without executing the
// request.
func (client *Client) InspectPolicy(request *http.Request) (ResolvedPolicy, error) {
	if request == nil {
		return ResolvedPolicy{}, ErrNilRequest
	}
	return ResolvePolicy(client.profile, client.policy, requestPolicyOverrides(request.Context()))
}

// LoadSession restores cookies through the configured persistence port.
func (client *Client) LoadSession(ctx context.Context) error {
	if client.session == nil {
		return ErrSessionDisabled
	}
	if ctx == nil {
		return fmt.Errorf("%w: load context is nil", ErrInvalidSession)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if client.context.Err() != nil {
		return ErrClientClosed
	}
	operationContext, cancel := context.WithCancel(ctx)
	stopClientCancellation := context.AfterFunc(client.context, cancel)
	defer func() {
		stopClientCancellation()
		cancel()
	}()

	err := client.session.load(operationContext)
	if client.context.Err() != nil {
		return ErrClientClosed
	}

	return err
}

// SaveSession persists cookies through the configured persistence port.
func (client *Client) SaveSession(ctx context.Context) error {
	if client.session == nil {
		return ErrSessionDisabled
	}
	if ctx == nil {
		return fmt.Errorf("%w: save context is nil", ErrInvalidSession)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if client.context.Err() != nil {
		return ErrClientClosed
	}
	operationContext, cancel := context.WithCancel(ctx)
	stopClientCancellation := context.AfterFunc(client.context, cancel)
	defer func() {
		stopClientCancellation()
		cancel()
	}()

	err := client.session.save(operationContext)
	if client.context.Err() != nil {
		return ErrClientClosed
	}

	return err
}

func (client *Client) do(request *http.Request, pipeline Pipeline) (*http.Response, error) {
	if request == nil {
		return nil, ErrNilRequest
	}

	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()

		return nil, ErrClientClosed
	}
	client.mu.Unlock()

	resolvedPolicy, err := client.InspectPolicy(request)
	if err != nil {
		return nil, err
	}
	requestContext := context.WithValue(request.Context(), resolvedPolicyContextKey{}, resolvedPolicy)
	requestContext, cancel := context.WithCancel(requestContext)
	stopLifecycleCancellation := context.AfterFunc(client.context, cancel)
	request = request.Clone(requestContext)

	response, err := pipeline.executeOperation(request, func(operationRequest *http.Request) (*http.Response, error) {
		standardClient := *client.httpClient
		if operationPolicy, ok := ResolvedPolicyFromContext(operationRequest.Context()); ok {
			standardClient.Timeout = operationPolicy.Values().OperationTimeout
		}
		transport := standardClient.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if client.egress != nil {
			transport = client.transport
			configuredRedirect := standardClient.CheckRedirect
			standardClient.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
				if err := client.egress.ValidateURL(redirect.URL); err != nil {
					return err
				}
				if configuredRedirect != nil {
					return configuredRedirect(redirect, via)
				}
				return nil
			}
			transport = egressRoundTripper{policy: client.egress, next: transport}
		}
		standardClient.Transport = attemptRoundTripper{
			pipeline:  pipeline,
			transport: transport,
		}

		operationResponse, operationErr := standardClient.Do(operationRequest)
		if operationErr != nil {
			return nil, newTransportError(operationRequest, operationErr)
		}

		return operationResponse, nil
	})
	if err != nil {
		stopLifecycleCancellation()
		cancel()

		return nil, err
	}

	body := &managedBody{
		ReadCloser:                response.Body,
		client:                    client,
		cancel:                    cancel,
		stopLifecycleCancellation: stopLifecycleCancellation,
	}
	response.Body = body

	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		_ = body.Close()

		return nil, ErrClientClosed
	}
	client.bodies[body] = struct{}{}
	client.mu.Unlock()

	return response, nil
}

type attemptRoundTripper struct {
	pipeline  Pipeline
	transport http.RoundTripper
}

func (transport attemptRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport.pipeline.executeAttempt(request, transport.transport.RoundTrip)
}

// CloseIdleConnections closes idle connections without changing client
// ownership or canceling active operations. This explicit call also applies to
// a borrowed transport.
func (client *Client) CloseIdleConnections() {
	if closer, ok := client.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// Close cancels pending operations, closes active response bodies, and closes
// idle connections on transports owned by the Client. It is idempotent.
func (client *Client) Close() error {
	client.closeOnce.Do(func() {
		client.mu.Lock()
		client.closed = true
		client.cancel()

		bodies := make([]*managedBody, 0, len(client.bodies))
		for body := range client.bodies {
			bodies = append(bodies, body)
		}
		client.mu.Unlock()

		var closeErrors []error
		for _, body := range bodies {
			if err := body.Close(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		if client.ownedTransport {
			client.CloseIdleConnections()
		}
		if client.session != nil {
			if client.session.saveOnClose {
				ctx, cancelSave := context.WithTimeout(context.Background(), client.session.timeout)
				closeErrors = append(closeErrors, client.session.save(ctx))
				cancelSave()
			}
			closeErrors = append(closeErrors, client.session.closeJar())
		}
		client.closeErr = errors.Join(closeErrors...)
	})

	return client.closeErr
}

func (client *Client) releaseBody(body *managedBody) {
	client.mu.Lock()
	delete(client.bodies, body)
	client.mu.Unlock()
}

func defaultTransport() *http.Transport {
	return defaultTransportWithEgress(nil)
}

func defaultTransportWithEgress(policy *EgressPolicy) *http.Transport {
	resolved, _ := ResolvePolicy(defaultPolicyProfile, PolicyOverrides{}, PolicyOverrides{})
	return defaultTransportWithPolicy(resolved.Values(), policy, nil)
}

func defaultTransportWithPolicy(
	values PolicyValues,
	egress *EgressPolicy,
	tlsPolicy *TLSPolicy,
) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   defaultConnectTimeout,
		KeepAlive: defaultKeepAlive,
	}

	dialContext := dialer.DialContext
	if egress != nil {
		dialContext = (&egressDialer{dialer: dialer, policy: egress}).DialContext
	}
	tlsConfig := &tls.Config{MinVersion: tlsMinimumVersion}
	if tlsPolicy != nil {
		tlsConfig = tlsPolicy.tlsConfig()
	}

	maximumIdlePerHost := min(defaultMaxIdleConnsPerHost, values.TransportMaximumConnections)
	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DisableCompression:     true,
		DialContext:            dialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           values.TransportMaximumConnections,
		MaxIdleConnsPerHost:    maximumIdlePerHost,
		MaxConnsPerHost:        values.TransportMaximumConnections,
		IdleConnTimeout:        defaultIdleConnTimeout,
		TLSHandshakeTimeout:    defaultTLSHandshakeTimeout,
		ResponseHeaderTimeout:  defaultResponseHeaderTimeout,
		ExpectContinueTimeout:  defaultExpectContinueTimeout,
		MaxResponseHeaderBytes: defaultMaxResponseHeaderSize,
		TLSClientConfig:        tlsConfig,
	}
}

// TransportError reports a failure before a usable HTTP response was
// returned. URL contains neither user information, query, nor fragment.
type TransportError struct {
	Method string
	URL    string
	Cause  error
}

// Error implements error without rendering the cause, which may contain
// credentials or query parameters copied by net/http.
func (err *TransportError) Error() string {
	return fmt.Sprintf("http transport %s %s failed", err.Method, err.URL)
}

// Unwrap returns the original transport failure.
func (err *TransportError) Unwrap() error {
	return err.Cause
}

func newTransportError(request *http.Request, cause error) *TransportError {
	return &TransportError{
		Method: request.Method,
		URL:    sanitizeURL(request.URL),
		Cause:  cause,
	}
}

func sanitizeURL(source *url.URL) string {
	if source == nil {
		return ""
	}

	sanitized := *source
	sanitized.User = nil
	sanitized.RawQuery = ""
	sanitized.ForceQuery = false
	sanitized.Fragment = ""
	sanitized.RawFragment = ""

	return sanitized.String()
}

type managedBody struct {
	io.ReadCloser
	client                    *Client
	cancel                    context.CancelFunc
	stopLifecycleCancellation func() bool
	once                      sync.Once
	err                       error
}

func (body *managedBody) Close() error {
	body.once.Do(func() {
		body.stopLifecycleCancellation()
		body.cancel()
		body.err = body.ReadCloser.Close()
		body.client.releaseBody(body)
	})

	return body.err
}
