package jwt

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

const defaultMaxJWKBodyBytes = 1024 * 1024

const defaultInitializationTimeout = 10 * time.Second

const defaultMaxJWKHeaderBytes int64 = 32 * 1024

const defaultMaxRemoteOperations = 128

const defaultRefreshJitter = 0.1

var errRemoteOperationLimit = errors.New("remote JWK operation limit exceeded")

type remoteConfig struct {
	client         *http.Client
	allowHTTP      bool
	minRefresh     time.Duration
	maxRefresh     time.Duration
	maxBodyBytes   int64
	maxHeaderBytes int64
	maxKeys        int
	refreshJitter  float64
	initTimeout    time.Duration
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

// WithRefreshJitter configures the maximum fractional deviation applied to
// provider refresh intervals. Zero disables jitter; values must be below one.
func WithRefreshJitter(maximumFraction float64) RemoteOption {
	return func(configuration *remoteConfig) { configuration.refreshJitter = maximumFraction }
}

// WithMaxJWKBodyBytes bounds a JWK HTTP response body.
func WithMaxJWKBodyBytes(maximum int64) RemoteOption {
	return func(configuration *remoteConfig) { configuration.maxBodyBytes = maximum }
}

// WithMaxJWKHeaderBytes bounds the aggregate response-header bytes accepted
// from the JWK endpoint.
func WithMaxJWKHeaderBytes(maximum int64) RemoteOption {
	return func(configuration *remoteConfig) { configuration.maxHeaderBytes = maximum }
}

// WithMaxJWKKeys bounds the number of keys accepted in a remote JWK set.
func WithMaxJWKKeys(maximum int) RemoteOption {
	return func(configuration *remoteConfig) { configuration.maxKeys = maximum }
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
	refreshing    bool
	refreshDone   chan struct{}
	refreshErr    error
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
		maxHeaderBytes: defaultMaxJWKHeaderBytes, maxKeys: defaultMaxKeys,
		refreshJitter: defaultRefreshJitter,
		initTimeout:   defaultInitializationTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(&configuration)
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: remote JWK configuration", authentication.ErrInvalidConfiguration)
	}
	if !validRemoteConfiguration(parsed, configuration) {
		return nil, fmt.Errorf("%w: remote JWK configuration", authentication.ErrInvalidConfiguration)
	}

	client := hardenedRemoteHTTPClient(configuration)
	initCtx, cancel := context.WithTimeout(ctx, configuration.initTimeout)
	defer cancel()
	_, err = jwk.Fetch(initCtx, rawURL,
		jwk.WithHTTPClient(client),
		jwk.WithFetchWhitelist(exactWhitelist(rawURL)),
		jwk.WithMaxFetchBodySize(configuration.maxBodyBytes),
	)
	switch err {
	case nil:
	default:
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

func validRemoteConfiguration(parsed *url.URL, configuration remoteConfig) bool {
	validScheme := parsed.Scheme == "https"
	if parsed.Scheme == "http" {
		validScheme = configuration.allowHTTP
	}
	return !slices.Contains([]bool{
		parsed.Host != "", parsed.User == nil, parsed.Fragment == "", validScheme,
		configuration.client != nil,
		cmp.Compare(configuration.minRefresh, time.Duration(0)) == 1,
		cmp.Compare(configuration.maxRefresh, configuration.minRefresh) != -1,
		cmp.Compare(configuration.maxBodyBytes, int64(0)) == 1,
		cmp.Compare(configuration.maxHeaderBytes, int64(0)) == 1,
		cmp.Compare(configuration.maxKeys, 0) == 1,
		configuration.refreshJitter >= 0 && configuration.refreshJitter < 1,
		cmp.Compare(configuration.initTimeout, time.Duration(0)) == 1,
	}, false)
}

func hardenedRemoteHTTPClient(configuration remoteConfig) *http.Client {
	client := jwk.WrapHTTPClientDefaults(configuration.client)
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("remote JWK redirects are disabled")
	}
	transport := client.Transport
	if standard, ok := transport.(*http.Transport); ok {
		cloned := standard.Clone()
		cloned.DisableCompression = true
		cloned.MaxResponseHeaderBytes = boundedResponseHeaderBytes(
			cloned.MaxResponseHeaderBytes,
			configuration.maxHeaderBytes,
		)
		transport = cloned
	}
	policy := &jwkResponseTransport{
		base: transport, maxBodyBytes: configuration.maxBodyBytes,
		maxHeaderBytes: configuration.maxHeaderBytes, maxKeys: configuration.maxKeys,
		minRefresh: configuration.minRefresh, maxRefresh: configuration.maxRefresh,
		refreshJitter: configuration.refreshJitter,
	}
	policy.jitterState.Store(rand.Uint64())
	client.Transport = policy
	return client
}

func boundedResponseHeaderBytes(current, maximum int64) int64 {
	if current <= 0 {
		return maximum
	}
	return min(current, maximum)
}

type jwkResponseTransport struct {
	base           http.RoundTripper
	maxBodyBytes   int64
	maxHeaderBytes int64
	maxKeys        int
	minRefresh     time.Duration
	maxRefresh     time.Duration
	refreshJitter  float64
	jitterState    atomic.Uint64
}

func (transport *jwkResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header = request.Header.Clone()
	request.Header.Set("Accept-Encoding", "identity")
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("remote JWK transport returned an invalid response")
	}
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	if responseHeaderBytes(response.Header) > transport.maxHeaderBytes {
		_ = response.Body.Close()
		return nil, errors.New("remote JWK response headers exceed configured bound")
	}
	if response.Header.Get("Content-Encoding") != "" {
		_ = response.Body.Close()
		return nil, errors.New("compressed remote JWK responses are not accepted")
	}
	body, err := readBoundedBody(response.Body, transport.maxBodyBytes)
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response, nil
	}
	if err := validateRemoteJWKBody(body, transport.maxKeys); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	transport.jitterRefreshHeader(response)
	return response, nil
}

func (transport *jwkResponseTransport) jitterRefreshHeader(response *http.Response) {
	if transport.refreshJitter == 0 {
		return
	}
	base := cacheLifetime(response.Header, time.Now(), transport.minRefresh, transport.maxRefresh)
	seed := mixJitter(transport.jitterState.Add(0x9e3779b97f4a7c15))
	interval := jitterDuration(base, transport.minRefresh, transport.maxRefresh, transport.refreshJitter, seed)
	seconds := int64(interval / time.Second)
	if interval%time.Second != 0 {
		seconds++
	}
	response.Header.Set("Cache-Control", "max-age="+strconv.FormatInt(seconds, 10))
	response.Header.Del("Expires")
}

func cacheLifetime(header http.Header, now time.Time, minimum, maximum time.Duration) time.Duration {
	for _, value := range header.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			name, encoded, found := strings.Cut(strings.TrimSpace(directive), "=")
			if !found {
				continue
			}
			if !strings.EqualFold(name, "max-age") {
				continue
			}
			seconds, err := strconv.ParseInt(strings.Trim(encoded, `"`), 10, 64)
			if err == nil {
				bounded := max(int64(0), min(seconds, int64(maximum/time.Second)))
				return clampDuration(time.Duration(bounded)*time.Second, minimum, maximum)
			}
		}
	}
	if expires, err := http.ParseTime(header.Get("Expires")); err == nil {
		return clampDuration(expires.Sub(now), minimum, maximum)
	}
	return minimum
}

func clampDuration(value, minimum, maximum time.Duration) time.Duration {
	return min(max(value, minimum), maximum)
}

func jitterDuration(base, minimum, maximum time.Duration, fraction float64, seed uint64) time.Duration {
	window := time.Duration(float64(base) * fraction)
	lower := max(base-window, minimum)
	upper := base + min(window, maximum-base)
	if upper <= lower {
		return lower
	}
	span := uint64(upper - lower)
	return lower + time.Duration(seed%span)
}

func mixJitter(value uint64) uint64 {
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func responseHeaderBytes(header http.Header) int64 {
	var size int64
	for name, values := range header {
		size += int64(len(name))
		for _, value := range values {
			size += int64(len(value))
		}
	}
	return size
}

func readBoundedBody(body io.ReadCloser, maximum int64) ([]byte, error) {
	defer func() { _ = body.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(body, maximum+1))
	if err != nil {
		return nil, errors.New("remote JWK response body could not be read")
	}
	if int64(len(encoded)) > maximum {
		return nil, errors.New("remote JWK response body exceeds configured bound")
	}
	return encoded, nil
}

func validateRemoteJWKBody(encoded []byte, maximum int) error {
	if err := inspectJSONObject(encoded, 64, 5); err != nil {
		return errors.New("remote JWK response is invalid")
	}
	set, err := jwk.Parse(encoded, jwk.WithRejectDuplicateKID(true))
	if err != nil || set.Len() == 0 || set.Len() > maximum {
		return errors.New("remote JWK response is invalid")
	}
	if err := validateJWKEntries(set, nil); err != nil {
		return errors.New("remote JWK response is invalid")
	}
	return nil
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
	return cloneKeySet(set)
}

// Refresh synchronously refreshes the cached JWK set. A failed refresh keeps
// the previously cached set available.
func (r *Remote) Refresh(ctx context.Context) error {
	operationCtx, cache, operation, err := r.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer r.endOperation(operation)
	if err := r.refresh(operationCtx, cache); err != nil {
		return authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return nil
}

func (r *Remote) refresh(ctx context.Context, cache *jwk.Cache) error {
	r.mutex.Lock()
	if r.refreshing {
		done := r.refreshDone
		r.mutex.Unlock()
		select {
		case <-done:
			r.mutex.Lock()
			err := r.refreshErr
			r.mutex.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.refreshing = true
	r.refreshDone = make(chan struct{})
	done := r.refreshDone
	r.mutex.Unlock()

	_, err := cache.Refresh(ctx, r.url)
	r.mutex.Lock()
	r.refreshing = false
	r.refreshErr = err
	close(done)
	r.mutex.Unlock()
	return err
}

func cloneKeySet(source jwk.Set) (jwk.Set, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	copied, err := jwk.Parse(encoded, jwk.WithRejectDuplicateKID(true))
	if err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return copied, nil
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
	switch err := ctx.Err(); err {
	case nil:
	default:
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}
	if r.closing {
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}
	if len(r.operations) >= defaultMaxRemoteOperations {
		return nil, nil, 0, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(errRemoteOperationLimit))
	}
	operationCtx, cancel := context.WithCancel(ctx)
	if r.operations == nil {
		r.operations = make(map[uint64]context.CancelFunc)
	}
	nextOperation, _ := bits.Add64(r.nextOperation, 1, 0)
	r.nextOperation = nextOperation
	r.operations[r.nextOperation] = cancel
	return operationCtx, r.cache, r.nextOperation, nil
}

func (r *Remote) endOperation(operation uint64) {
	r.mutex.Lock()
	cancel := r.operations[operation]
	delete(r.operations, operation)
	if r.closing {
		if len(r.operations) == 0 {
			if r.idle != nil {
				close(r.idle)
				r.idle = nil
			}
		}
	}
	r.mutex.Unlock()
	switch cancel {
	case nil:
	default:
		cancel()
	}
}

func (r *Remote) finishClose(done chan struct{}, err error) {
	r.mutex.Lock()
	r.closing = false
	r.closeErr = err
	r.idle = nil
	switch err {
	case nil:
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
