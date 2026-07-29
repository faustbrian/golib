package oidc

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
	jose "github.com/go-jose/go-jose/v4"
)

var errHTTPBodyTooLarge = errors.New("OIDC HTTP response exceeds configured bound")

var errOIDCRefreshBusy = errors.New("OIDC JWK refresh waiter limit exceeded")

// New discovers an OIDC provider and creates a synchronous, bounded key-set
// validator. It starts no background goroutines.
func New(ctx context.Context, configuration Config) (*Validator, error) {
	if err := ctx.Err(); err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	applyDefaults(&configuration)
	algorithms, err := validateConfig(configuration)
	if err != nil {
		return nil, err
	}
	client := hardenedClient(configuration.HTTPClient, configuration.MaxHTTPBodyBytes)
	discoveryContext, cancel := context.WithTimeout(ctx, configuration.DiscoveryTimeout)
	defer cancel()
	discoveryContext = upstreamoidc.ClientContext(discoveryContext, client)
	provider, err := upstreamoidc.NewProvider(discoveryContext, configuration.Issuer)
	if err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	var metadata struct {
		JWKSetURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil || !validRemoteURL(metadata.JWKSetURL, configuration.InsecureHTTP) {
		return nil, fmt.Errorf("%w: OIDC discovery metadata", authentication.ErrInvalidConfiguration)
	}
	keySet := &remoteKeySet{
		url: metadata.JWKSetURL, client: client,
		algorithms: joseAlgorithms(configuration.Algorithms), allowed: algorithms,
		maxBodyBytes: configuration.MaxHTTPBodyBytes, maxKeys: configuration.MaxKeys,
		clock:              configuration.Clock,
		minRefreshInterval: configuration.MinRefreshInterval,
		maxRefreshInterval: configuration.MaxRefreshInterval,
		waiters:            make(chan struct{}, configuration.MaxRefreshWaiters),
	}
	return NewWithKeySet(configuration, keySet)
}

type remoteKeySet struct {
	url                string
	client             *http.Client
	algorithms         []jose.SignatureAlgorithm
	allowed            map[string]struct{}
	maxBodyBytes       int64
	maxKeys            int
	clock              Clock
	minRefreshInterval time.Duration
	maxRefreshInterval time.Duration
	waiters            chan struct{}

	mutex        sync.Mutex
	keys         []jose.JSONWebKey
	refreshing   bool
	refreshDone  chan struct{}
	nextRefresh  time.Time
	refreshErr   error
	etag         string
	lastModified string
}

func (set *remoteKeySet) VerifySignature(ctx context.Context, rawToken string) ([]byte, error) {
	signed, err := jose.ParseSigned(rawToken, set.algorithms)
	if err != nil {
		return nil, errors.New("invalid OIDC signature structure")
	}
	switch len(signed.Signatures) {
	case 1:
	default:
		return nil, errors.New("invalid OIDC signature structure")
	}
	keyID := signed.Signatures[0].Header.KeyID
	if keyID == "" {
		return nil, errors.New("OIDC key ID is required")
	}

	set.mutex.Lock()
	keys := append([]jose.JSONWebKey(nil), set.keys...)
	set.mutex.Unlock()
	if payload, found, err := verifyWithKeys(signed, keyID, keys); found {
		return payload, err
	}
	switch err := set.acquireWaiter(ctx); err {
	case nil:
	default:
		reportUnavailable(ctx, err)
		return nil, err
	}
	defer set.releaseWaiter()

	for {
		payload, found, err := set.verifyCurrent(signed, keyID)
		if found {
			return payload, err
		}

		set.mutex.Lock()
		now := set.now()
		if set.refreshing {
			done := set.refreshDone
			set.mutex.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				reportUnavailable(ctx, ctx.Err())
				return nil, ctx.Err()
			}
		}
		if !set.nextRefresh.IsZero() && now.Before(set.nextRefresh) {
			refreshErr := set.refreshErr
			set.mutex.Unlock()
			switch refreshErr {
			case nil:
			default:
				reportUnavailable(ctx, refreshErr)
				return nil, errors.New("OIDC keys unavailable")
			}
			return nil, errors.New("OIDC key ID not found")
		}

		set.refreshing = true
		set.refreshDone = make(chan struct{})
		etag, lastModified := set.etag, set.lastModified
		set.mutex.Unlock()

		result, fetchErr := set.fetchConditional(ctx, etag, lastModified)
		set.finishRefresh(now, result, fetchErr)
		if fetchErr != nil {
			reportUnavailable(ctx, fetchErr)
			return nil, errors.New("OIDC keys unavailable")
		}
	}
}

func (set *remoteKeySet) acquireWaiter(ctx context.Context) error {
	switch set.waiters {
	case nil:
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case set.waiters <- struct{}{}:
		return nil
	default:
		return errOIDCRefreshBusy
	}
}

func (set *remoteKeySet) releaseWaiter() {
	if set.waiters != nil {
		<-set.waiters
	}
}

func (set *remoteKeySet) verifyCurrent(
	signed *jose.JSONWebSignature,
	keyID string,
) ([]byte, bool, error) {
	set.mutex.Lock()
	keys := append([]jose.JSONWebKey(nil), set.keys...)
	set.mutex.Unlock()
	return verifyWithKeys(signed, keyID, keys)
}

func (set *remoteKeySet) finishRefresh(started time.Time, result fetchResult, err error) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	minimum, maximum := set.refreshBounds()
	set.refreshErr = err
	set.nextRefresh = started.Add(minimum)
	if err == nil {
		if !result.notModified {
			set.keys = result.keys
		}
		if result.etag != "" {
			set.etag = result.etag
		}
		if result.lastModified != "" {
			set.lastModified = result.lastModified
		}
		set.nextRefresh = started.Add(cacheLifetime(result.header, minimum, maximum))
	}
	set.refreshing = false
	close(set.refreshDone)
}

func (set *remoteKeySet) now() time.Time {
	if set.clock == nil {
		return (clockpkg.System{}).Now()
	}
	return set.clock.Now()
}

func (set *remoteKeySet) refreshBounds() (time.Duration, time.Duration) {
	minimum, maximum := set.minRefreshInterval, set.maxRefreshInterval
	if minimum <= 0 {
		minimum = time.Minute
	}
	switch cmp.Compare(maximum, minimum) {
	case -1:
		maximum = max(time.Hour, minimum)
	}
	return minimum, maximum
}

func verifyWithKeys(signed *jose.JSONWebSignature, keyID string, keys []jose.JSONWebKey) ([]byte, bool, error) {
	found := false
	for _, key := range keys {
		if key.KeyID == keyID {
			found = true
			if payload, err := signed.Verify(key.Key); err == nil {
				return payload, true, nil
			}
		}
	}
	if found {
		return nil, true, errors.New("OIDC signature rejected")
	}
	return nil, false, errors.New("OIDC key ID not found")
}

func (set *remoteKeySet) fetch(ctx context.Context) ([]jose.JSONWebKey, error) {
	result, err := set.fetchConditional(ctx, "", "")
	switch err {
	case nil:
	default:
		return nil, err
	}
	return result.keys, nil
}

type fetchResult struct {
	keys         []jose.JSONWebKey
	notModified  bool
	header       http.Header
	etag         string
	lastModified string
}

func (set *remoteKeySet) fetchConditional(
	ctx context.Context,
	etag string,
	lastModified string,
) (fetchResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, set.url, nil)
	switch err {
	case nil:
	default:
		return fetchResult{}, err
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		request.Header.Set("If-Modified-Since", lastModified)
	}
	response, err := set.client.Do(request)
	if err != nil {
		return fetchResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	result := fetchResult{
		header:       response.Header.Clone(),
		etag:         response.Header.Get("ETag"),
		lastModified: response.Header.Get("Last-Modified"),
	}
	switch response.StatusCode {
	case http.StatusNotModified:
		switch etag {
		case "":
		default:
			result.notModified = true
			return result, nil
		}
		switch lastModified {
		case "":
		default:
			result.notModified = true
			return result, nil
		}
		return fetchResult{}, errors.New("OIDC JWK endpoint returned a non-success status")
	case http.StatusOK:
	default:
		return fetchResult{}, errors.New("OIDC JWK endpoint returned a non-success status")
	}
	body, err := readBounded(response.Body, set.maxBodyBytes)
	if err != nil {
		return fetchResult{}, err
	}
	var parsed jose.JSONWebKeySet
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fetchResult{}, errors.New("OIDC JWK response is invalid")
	}
	if len(parsed.Keys) == 0 || len(parsed.Keys) > set.maxKeys {
		return fetchResult{}, errors.New("OIDC JWK count is invalid")
	}
	seen := make(map[string]struct{}, len(parsed.Keys))
	keys := make([]jose.JSONWebKey, len(parsed.Keys))
	for index, key := range parsed.Keys {
		if !validJWKMetadata(key) {
			return fetchResult{}, errors.New("OIDC JWK metadata is invalid")
		}
		if _, duplicate := seen[key.KeyID]; duplicate {
			return fetchResult{}, errors.New("OIDC JWK IDs are ambiguous")
		}
		if _, allowed := set.allowed[key.Algorithm]; !allowed {
			return fetchResult{}, errors.New("OIDC JWK algorithm is disallowed")
		}
		if !joseKeyMatchesAlgorithm(key.Key, key.Algorithm) {
			return fetchResult{}, errors.New("OIDC JWK key type is invalid")
		}
		seen[key.KeyID] = struct{}{}
		keys[index] = key
	}
	result.keys = keys
	return result, nil
}

func validJWKMetadata(key jose.JSONWebKey) bool {
	return !slices.Contains([]bool{
		key.KeyID != "", key.Valid(), key.IsPublic(), key.Algorithm != "", key.Use == "sig",
	}, false)
}

func joseKeyMatchesAlgorithm(key any, algorithm string) bool {
	switch {
	case strings.HasPrefix(algorithm, "RS"), strings.HasPrefix(algorithm, "PS"):
		_, ok := key.(*rsa.PublicKey)
		return ok
	case strings.HasPrefix(algorithm, "ES"):
		publicKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return false
		}
		expectedBits := map[string]int{"ES256": 256, "ES384": 384, "ES512": 521}[algorithm]
		return expectedBits != 0 && publicKey.Curve.Params().BitSize == expectedBits
	case algorithm == "EdDSA":
		_, ok := key.(ed25519.PublicKey)
		return ok
	default:
		return false
	}
}

func cacheLifetime(header http.Header, minimum, maximum time.Duration) time.Duration {
	lifetime := minimum
	foundMaxAge := false
	for _, value := range header.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			seconds, state := cacheDirective(directive, maximum)
			switch state {
			case cacheDirectiveDisable:
				return minimum
			case cacheDirectiveMaxAge:
				foundMaxAge = true
				lifetime = seconds
			}
		}
	}
	if !foundMaxAge {
		date, dateErr := http.ParseTime(header.Get("Date"))
		expires, expiresErr := http.ParseTime(header.Get("Expires"))
		if dateErr == nil {
			if expiresErr == nil {
				if expires.After(date) {
					lifetime = expires.Sub(date)
				}
			}
		}
	}
	if age, err := strconv.ParseInt(header.Get("Age"), 10, 64); err == nil {
		switch cmp.Compare(age, int64(0)) {
		case 1:
			switch cmp.Compare(age, int64(lifetime/time.Second)) {
			case 0, 1:
				lifetime = 0
			default:
				lifetime = lifetime - time.Duration(age)*time.Second
			}
		}
	}
	return min(max(lifetime, minimum), maximum)
}

type cacheDirectiveState uint8

const (
	cacheDirectiveIgnore cacheDirectiveState = iota
	cacheDirectiveDisable
	cacheDirectiveMaxAge
)

func cacheDirective(directive string, maximum time.Duration) (time.Duration, cacheDirectiveState) {
	name, parameter, hasParameter := strings.Cut(strings.TrimSpace(directive), "=")
	switch {
	case strings.EqualFold(name, "no-cache"), strings.EqualFold(name, "no-store"):
		return 0, cacheDirectiveDisable
	case !strings.EqualFold(name, "max-age"), !hasParameter:
		return 0, cacheDirectiveIgnore
	}
	seconds, err := strconv.ParseInt(strings.Trim(parameter, `"`), 10, 64)
	if err != nil {
		return 0, cacheDirectiveIgnore
	}
	switch cmp.Compare(seconds, int64(0)) {
	case -1:
		return 0, cacheDirectiveIgnore
	}
	maximumSeconds := int64(maximum.Seconds())
	switch cmp.Compare(seconds, maximumSeconds) {
	case 0, 1:
		return maximum, cacheDirectiveMaxAge
	default:
		return time.Duration(seconds) * time.Second, cacheDirectiveMaxAge
	}
}

func joseAlgorithms(names []string) []jose.SignatureAlgorithm {
	algorithms := make([]jose.SignatureAlgorithm, len(names))
	for index, name := range names {
		algorithms[index] = jose.SignatureAlgorithm(name)
	}
	return algorithms
}

func validRemoteURL(rawURL string, allowHTTP bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return allowHTTP
	default:
		return false
	}
}

func hardenedClient(source *http.Client, maximum int64) *http.Client {
	client := &http.Client{}
	if source != nil {
		*client = *source
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("OIDC redirects are disabled")
	}
	transport := client.Transport
	switch transport {
	case nil:
		transport = http.DefaultTransport
	}
	client.Transport = boundedTransport{base: transport, maximum: maximum}
	return client
}

type boundedTransport struct {
	base    http.RoundTripper
	maximum int64
}

func (transport boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedBody{body: response.Body, remaining: transport.maximum}
	return response, nil
}

type boundedBody struct {
	body      io.ReadCloser
	remaining int64
	exceeded  bool
}

func (body *boundedBody) Read(buffer []byte) (int, error) {
	if body.exceeded {
		return 0, errHTTPBodyTooLarge
	}
	probe, _ := bits.Add64(uint64(body.remaining), 1, 0)
	limit := min(int64(len(buffer)), int64(probe))
	read, err := body.body.Read(buffer[:limit])
	if int64(read) > body.remaining {
		allowed := int(body.remaining)
		body.remaining = 0
		body.exceeded = true
		return allowed, errHTTPBodyTooLarge
	}
	body.remaining = body.remaining - int64(read)
	return read, err
}

func (body *boundedBody) Close() error { return body.body.Close() }

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	limited := io.LimitReader(reader, maximum+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maximum {
		return nil, errHTTPBodyTooLarge
	}
	return body, nil
}

var _ upstreamoidc.KeySet = (*remoteKeySet)(nil)
