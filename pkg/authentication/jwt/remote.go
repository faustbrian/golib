package jwt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

const defaultMaxJWKBodyBytes = 1024 * 1024

const defaultInitializationTimeout = 10 * time.Second

type remoteConfig struct {
	client       *http.Client
	allowHTTP    bool
	minRefresh   time.Duration
	maxRefresh   time.Duration
	maxBodyBytes int64
	initTimeout  time.Duration
}

// RemoteOption configures a network-backed JWK provider.
type RemoteOption func(*remoteConfig)

// WithHTTPClient supplies an HTTP client. JWX timeout and redirect hardening
// is layered onto a shallow copy of the client.
func WithHTTPClient(client *http.Client) RemoteOption {
	return func(configuration *remoteConfig) { configuration.client = client }
}

// WithInsecureHTTP permits an HTTP JWK URL. It is intended only for isolated
// tests and trusted development networks.
func WithInsecureHTTP() RemoteOption {
	return func(configuration *remoteConfig) { configuration.allowHTTP = true }
}

// WithRefreshBounds configures minimum and maximum automatic refresh intervals.
func WithRefreshBounds(minimum, maximum time.Duration) RemoteOption {
	return func(configuration *remoteConfig) {
		configuration.minRefresh = minimum
		configuration.maxRefresh = maximum
	}
}

// WithMaxJWKBodyBytes bounds a JWK HTTP response body.
func WithMaxJWKBodyBytes(maximum int64) RemoteOption {
	return func(configuration *remoteConfig) { configuration.maxBodyBytes = maximum }
}

// WithInitializationTimeout bounds the initial fetch and cache registration.
func WithInitializationTimeout(timeout time.Duration) RemoteOption {
	return func(configuration *remoteConfig) { configuration.initTimeout = timeout }
}

// Remote owns a bounded JWK cache and all of its background goroutines.
type Remote struct {
	cache         *jwk.Cache
	url           string
	mutex         sync.Mutex
	closed        bool
	closing       bool
	closeDone     chan struct{}
	closeErr      error
	nextOperation uint64
	operations    map[uint64]context.CancelFunc
	idle          chan struct{}
}

// NewRemote registers and initially fetches one exact JWK URL. The caller owns
// the returned provider and must call Close.
func NewRemote(ctx context.Context, rawURL string, options ...RemoteOption) (*Remote, error) {
	if err := ctx.Err(); err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	configuration := remoteConfig{
		client: &http.Client{}, minRefresh: time.Minute,
		maxRefresh: time.Hour, maxBodyBytes: defaultMaxJWKBodyBytes,
		initTimeout: defaultInitializationTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(&configuration)
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && (!configuration.allowHTTP || parsed.Scheme != "http")) ||
		configuration.client == nil || configuration.minRefresh <= 0 ||
		configuration.maxRefresh < configuration.minRefresh || configuration.maxBodyBytes <= 0 ||
		configuration.initTimeout <= 0 {
		return nil, fmt.Errorf("%w: remote JWK configuration", authentication.ErrInvalidConfiguration)
	}

	client := jwk.WrapHTTPClientDefaults(configuration.client)
	initCtx, cancel := context.WithTimeout(ctx, configuration.initTimeout)
	defer cancel()
	if _, err := jwk.Fetch(initCtx, rawURL,
		jwk.WithHTTPClient(client),
		jwk.WithFetchWhitelist(exactWhitelist(rawURL)),
		jwk.WithMaxFetchBodySize(configuration.maxBodyBytes),
	); err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	resourceClient := httprc.NewClient(
		httprc.WithHTTPClient(client),
		httprc.WithWhitelist(exactWhitelist(rawURL)),
	)
	// A newly constructed httprc client has not been started, which is the only
	// condition under which this upstream constructor reports an error.
	cache, _ := jwk.NewCache(ctx, resourceClient)
	if err := cache.Register(initCtx, rawURL,
		jwk.WithWaitReady(true),
		jwk.WithMinInterval(configuration.minRefresh),
		jwk.WithMaxInterval(configuration.maxRefresh),
		jwk.WithMaxFetchBodySize(configuration.maxBodyBytes),
	); err != nil {
		_ = cache.Shutdown(context.Background())
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return &Remote{cache: cache, url: rawURL}, nil
}

// KeySet returns the current cached JWK set without transferring ownership.
func (r *Remote) KeySet(ctx context.Context) (jwk.Set, error) {
	operationCtx, cache, operation, err := r.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer r.endOperation(operation)
	set, err := cache.Lookup(operationCtx, r.url)
	if err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return set, nil
}

// Refresh synchronously refreshes the cached JWK set. A failed refresh keeps
// the previously cached set available.
func (r *Remote) Refresh(ctx context.Context) error {
	operationCtx, cache, operation, err := r.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer r.endOperation(operation)
	if _, err := cache.Refresh(operationCtx, r.url); err != nil {
		return authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return nil
}

// Close cancels and joins all cache-owned background work.
func (r *Remote) Close(ctx context.Context) error {
	r.mutex.Lock()
	if r.closed {
		r.mutex.Unlock()
		return nil
	}
	if r.closing {
		done := r.closeDone
		r.mutex.Unlock()
		select {
		case <-done:
			return r.closeResult()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.closing = true
	r.closeDone = make(chan struct{})
	done := r.closeDone
	r.closeErr = nil
	r.idle = make(chan struct{})
	idle := r.idle
	cancels := make([]context.CancelFunc, 0, len(r.operations))
	for _, cancel := range r.operations {
		cancels = append(cancels, cancel)
	}
	if len(r.operations) == 0 {
		close(r.idle)
	}
	r.mutex.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	select {
	case <-idle:
	case <-ctx.Done():
		err := ctx.Err()
		r.finishClose(done, err)
		return err
	}

	err := r.cache.Shutdown(ctx)
	r.finishClose(done, err)
	return err
}

func (r *Remote) beginOperation(ctx context.Context) (context.Context, *jwk.Cache, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed || r.closing {
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}
	operationCtx, cancel := context.WithCancel(ctx)
	if r.operations == nil {
		r.operations = make(map[uint64]context.CancelFunc)
	}
	r.nextOperation++
	r.operations[r.nextOperation] = cancel
	return operationCtx, r.cache, r.nextOperation, nil
}

func (r *Remote) endOperation(operation uint64) {
	r.mutex.Lock()
	cancel := r.operations[operation]
	delete(r.operations, operation)
	if r.closing && len(r.operations) == 0 && r.idle != nil {
		close(r.idle)
		r.idle = nil
	}
	r.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Remote) finishClose(done chan struct{}, err error) {
	r.mutex.Lock()
	r.closing = false
	r.closeErr = err
	r.idle = nil
	if err == nil {
		r.closed = true
	}
	close(done)
	r.mutex.Unlock()
}

func (r *Remote) closeResult() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return nil
	}
	return r.closeErr
}

type exactWhitelist string

func (allowed exactWhitelist) IsAllowed(candidate string) bool { return string(allowed) == candidate }

var _ KeyProvider = (*Remote)(nil)
