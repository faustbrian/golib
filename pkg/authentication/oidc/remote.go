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
	"math/rand/v2"
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

var errOIDCDiscoveryUnavailable = errors.New("OIDC discovery unavailable")

var errOIDCMetadataInvalid = errors.New("OIDC discovery metadata is invalid")

var errOIDCKeysUnavailable = errors.New("OIDC keys unavailable")

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
	metadata, err := discoverProvider(
		discoveryContext,
		configuration.Issuer,
		client,
		configuration.InsecureHTTP,
		algorithms,
	)
	if err != nil {
		if errors.Is(err, errOIDCMetadataInvalid) {
			return nil, fmt.Errorf("%w: OIDC discovery metadata", authentication.ErrInvalidConfiguration)
		}
		cause := errOIDCDiscoveryUnavailable
		if contextErr := discoveryContext.Err(); contextErr != nil {
			cause = contextErr
		}
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(cause))
	}
	keySet := &remoteKeySet{
		url: metadata.JWKSetURL, client: client,
		issuer: configuration.Issuer, insecureHTTP: configuration.InsecureHTTP,
		algorithms: joseAlgorithms(configuration.Algorithms), allowed: algorithms,
		maxBodyBytes: configuration.MaxHTTPBodyBytes, maxKeys: configuration.MaxKeys,
		jitter:             rand.Uint64(),
		clock:              configuration.Clock,
		minRefreshInterval: configuration.MinRefreshInterval,
		maxRefreshInterval: configuration.MaxRefreshInterval,
		waiters:            make(chan struct{}, configuration.MaxRefreshWaiters),
	}
	if err := keySet.initialize(discoveryContext); err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(redactedProviderError(discoveryContext, err)))
	}
	return NewWithKeySet(configuration, keySet)
}

func discoverProvider(
	ctx context.Context,
	issuer string,
	client *http.Client,
	insecureHTTP bool,
	allowed map[string]struct{},
) (providerMetadata, error) {
	providerContext := upstreamoidc.ClientContext(ctx, client)
	provider, err := upstreamoidc.NewProvider(providerContext, issuer)
	if err != nil {
		var mismatch *upstreamoidc.IssuerMismatchError
		if errors.As(err, &mismatch) {
			return providerMetadata{}, errOIDCMetadataInvalid
		}
		return providerMetadata{}, errOIDCDiscoveryUnavailable
	}
	var metadata providerMetadata
	if err := provider.Claims(&metadata); err != nil || !validProviderMetadata(metadata, insecureHTTP, allowed) {
		return providerMetadata{}, errOIDCMetadataInvalid
	}
	return metadata, nil
}

type providerMetadata struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSetURL             string   `json:"jwks_uri"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	Scopes                []string `json:"scopes_supported"`
	ResponseTypes         []string `json:"response_types_supported"`
	SubjectTypes          []string `json:"subject_types_supported"`
	SigningAlgorithms     []string `json:"id_token_signing_alg_values_supported"`
}

func validProviderMetadata(metadata providerMetadata, insecureHTTP bool, allowed map[string]struct{}) bool {
	if !validRemoteURL(metadata.AuthorizationEndpoint, insecureHTTP) ||
		!validRemoteURL(metadata.JWKSetURL, insecureHTTP) {
		return false
	}
	if metadata.TokenEndpoint != "" && !validRemoteURL(metadata.TokenEndpoint, insecureHTTP) {
		return false
	}
	if metadata.UserInfoEndpoint != "" && !validRemoteURL(metadata.UserInfoEndpoint, insecureHTTP) {
		return false
	}
	if metadata.RegistrationEndpoint != "" && !validRemoteURL(metadata.RegistrationEndpoint, insecureHTTP) {
		return false
	}
	responseTypes, valid := uniqueStrings(metadata.ResponseTypes)
	if !valid {
		return false
	}
	requiresTokenEndpoint := false
	for responseType := range responseTypes {
		parts := strings.Fields(responseType)
		if len(parts) == 0 {
			return false
		}
		if slices.Contains(parts, "code") {
			requiresTokenEndpoint = true
		}
	}
	if requiresTokenEndpoint && metadata.TokenEndpoint == "" {
		return false
	}
	subjectTypes, valid := uniqueStrings(metadata.SubjectTypes)
	if !valid {
		return false
	}
	for subjectType := range subjectTypes {
		if subjectType != "public" && subjectType != "pairwise" {
			return false
		}
	}
	algorithms, valid := uniqueStrings(metadata.SigningAlgorithms)
	if !valid {
		return false
	}
	if _, required := algorithms["RS256"]; !required {
		return false
	}
	for algorithm := range allowed {
		if _, advertised := algorithms[algorithm]; !advertised {
			return false
		}
	}
	if len(metadata.Scopes) > 0 {
		scopes, scopesValid := uniqueStrings(metadata.Scopes)
		if !scopesValid || !slices.Contains(metadata.Scopes, "openid") || len(scopes) == 0 {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) (map[string]struct{}, bool) {
	if len(values) == 0 {
		return nil, false
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return nil, false
		}
		if _, duplicate := unique[value]; duplicate {
			return nil, false
		}
		unique[value] = struct{}{}
	}
	return unique, true
}

type remoteKeySet struct {
	url                string
	issuer             string
	insecureHTTP       bool
	client             *http.Client
	algorithms         []jose.SignatureAlgorithm
	allowed            map[string]struct{}
	maxBodyBytes       int64
	maxKeys            int
	jitter             uint64
	clock              Clock
	minRefreshInterval time.Duration
	maxRefreshInterval time.Duration
	waiters            chan struct{}

	mutex        sync.Mutex
	keys         []jose.JSONWebKey
	refreshing   bool
	refreshDone  chan struct{}
	nextRefresh  time.Time
	nextAttempt  time.Time
	refreshErr   error
	etag         string
	lastModified string
}

func (set *remoteKeySet) initialize(ctx context.Context) error {
	set.mutex.Lock()
	set.refreshing = true
	set.refreshDone = make(chan struct{})
	started := set.now()
	set.mutex.Unlock()

	result, err := set.fetchConditional(ctx, "", "")
	set.finishRefresh(started, result, err)
	return err
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
	set.mutex.Lock()
	now := set.now()
	keys := append([]jose.JSONWebKey(nil), set.keys...)
	fresh := now.Before(set.nextRefresh) || set.nextRefresh.IsZero() && len(keys) > 0
	nextAttempt := set.nextAttempt
	refreshErr := set.refreshErr
	set.mutex.Unlock()
	if fresh {
		if payload, found, verifyErr := verifyWithKeys(signed, keyID, keys); found {
			return payload, verifyErr
		}
		if now.Before(nextAttempt) {
			if refreshErr != nil {
				reportUnavailable(ctx, redactedProviderError(ctx, refreshErr))
				return nil, errors.New("OIDC keys unavailable")
			}
			return nil, errors.New("OIDC key ID not found")
		}
	}

	switch err := set.acquireWaiter(ctx); err {
	case nil:
	default:
		reportUnavailable(ctx, err)
		return nil, err
	}
	defer set.releaseWaiter()

	for {
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
		keys := append([]jose.JSONWebKey(nil), set.keys...)
		nextRefresh := set.nextRefresh
		fresh := now.Before(nextRefresh) || nextRefresh.IsZero() && len(keys) > 0
		nextAttempt := set.nextAttempt
		refreshErr := set.refreshErr
		set.mutex.Unlock()
		if fresh {
			if payload, found, err := verifyWithKeys(signed, keyID, keys); found {
				return payload, err
			}
		}
		if now.Before(nextAttempt) {
			if refreshErr != nil || !fresh {
				reportUnavailable(ctx, redactedProviderError(ctx, refreshErr))
				return nil, errors.New("OIDC keys unavailable")
			}
			return nil, errors.New("OIDC key ID not found")
		}

		set.mutex.Lock()
		claimed := !refreshStateChanged(set.refreshing, set.nextAttempt, set.nextRefresh, nextAttempt, nextRefresh)
		var etag, lastModified string
		if claimed {
			set.refreshing = true
			set.refreshDone = make(chan struct{})
			etag, lastModified = set.etag, set.lastModified
		}
		set.mutex.Unlock()

		if claimed {
			result, fetchErr := set.refresh(ctx, etag, lastModified)
			set.finishRefresh(now, result, fetchErr)
			if fetchErr != nil {
				reportUnavailable(ctx, redactedProviderError(ctx, fetchErr))
				return nil, errors.New("OIDC keys unavailable")
			}
		}
	}
}

func refreshStateChanged(
	refreshing bool,
	nextAttempt time.Time,
	nextRefresh time.Time,
	observedAttempt time.Time,
	observedRefresh time.Time,
) bool {
	return refreshing || !nextAttempt.Equal(observedAttempt) || !nextRefresh.Equal(observedRefresh)
}

func redactedProviderError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errOIDCKeysUnavailable
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

func (set *remoteKeySet) finishRefresh(started time.Time, result fetchResult, err error) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	minimum, maximum := set.refreshBounds()
	set.refreshErr = err
	set.nextAttempt = started.Add(minimum)
	if err == nil {
		if !result.notModified {
			set.keys = result.keys
			set.etag = result.etag
			set.lastModified = result.lastModified
			if result.jwkSetURL != "" {
				set.url = result.jwkSetURL
			}
		} else {
			if result.etag != "" {
				set.etag = result.etag
			}
			if result.lastModified != "" {
				set.lastModified = result.lastModified
			}
		}
		lifetime := cacheLifetime(result.header, minimum, maximum)
		set.nextRefresh = started.Add(set.refreshLifetime(lifetime, minimum))
	}
	set.refreshing = false
	close(set.refreshDone)
}

func (set *remoteKeySet) refreshLifetime(lifetime, minimum time.Duration) time.Duration {
	window := (lifetime - minimum) / 10
	if window <= 0 {
		return lifetime
	}
	offset := time.Duration(set.jitter % uint64(window+1))
	return lifetime - offset
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
	algorithm := signed.Signatures[0].Header.Algorithm
	for _, key := range keys {
		if keyID != "" && key.KeyID != keyID {
			continue
		}
		if key.Algorithm != "" && key.Algorithm != algorithm {
			continue
		}
		if !joseKeyMatchesAlgorithm(key.Key, algorithm) {
			continue
		}
		found = true
		if payload, err := signed.Verify(key.Key); err == nil {
			return payload, true, nil
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

func (set *remoteKeySet) refresh(ctx context.Context, etag, lastModified string) (fetchResult, error) {
	set.mutex.Lock()
	url := set.url
	set.mutex.Unlock()
	if set.issuer != "" {
		metadata, err := discoverProvider(ctx, set.issuer, set.client, set.insecureHTTP, set.allowed)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return fetchResult{}, contextErr
			}
			return fetchResult{}, errors.New("OIDC discovery refresh failed")
		}
		if metadata.JWKSetURL != url {
			url = metadata.JWKSetURL
			etag = ""
			lastModified = ""
		}
	}
	result, err := set.fetchConditionalURL(ctx, url, etag, lastModified)
	result.jwkSetURL = url
	return result, err
}

type fetchResult struct {
	keys         []jose.JSONWebKey
	notModified  bool
	header       http.Header
	etag         string
	lastModified string
	jwkSetURL    string
}

func (set *remoteKeySet) fetchConditional(
	ctx context.Context,
	etag string,
	lastModified string,
) (fetchResult, error) {
	return set.fetchConditionalURL(ctx, set.url, etag, lastModified)
}

func (set *remoteKeySet) fetchConditionalURL(
	ctx context.Context,
	rawURL string,
	etag string,
	lastModified string,
) (fetchResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
		if !validJWKMetadata(key) || !jwkMatchesAllowedAlgorithms(key, set.allowed) {
			return fetchResult{}, errors.New("OIDC JWK metadata is invalid")
		}
		if key.KeyID != "" {
			if _, duplicate := seen[key.KeyID]; duplicate {
				return fetchResult{}, errors.New("OIDC JWK IDs are ambiguous")
			}
			seen[key.KeyID] = struct{}{}
		}
		keys[index] = key
	}
	result.keys = keys
	return result, nil
}

func validJWKMetadata(key jose.JSONWebKey) bool {
	return !slices.Contains([]bool{
		key.Valid(), key.IsPublic(), key.Use == "" || key.Use == "sig",
	}, false)
}

func jwkMatchesAllowedAlgorithms(key jose.JSONWebKey, allowed map[string]struct{}) bool {
	if key.Algorithm != "" {
		_, permitted := allowed[key.Algorithm]
		return permitted && joseKeyMatchesAlgorithm(key.Key, key.Algorithm)
	}
	for algorithm := range allowed {
		if joseKeyMatchesAlgorithm(key.Key, algorithm) {
			return true
		}
	}
	return false
}

func joseKeyMatchesAlgorithm(key any, algorithm string) bool {
	switch {
	case strings.HasPrefix(algorithm, "RS"), strings.HasPrefix(algorithm, "PS"):
		publicKey, ok := key.(*rsa.PublicKey)
		return ok && publicKey.N != nil && publicKey.N.BitLen() >= 2048 && publicKey.N.BitLen() <= 8192
	case strings.HasPrefix(algorithm, "ES"):
		publicKey, ok := key.(*ecdsa.PublicKey)
		if !ok || publicKey.Curve == nil {
			return false
		}
		if !validECDSAPublicKey(publicKey) {
			return false
		}
		expectedBits := map[string]int{"ES256": 256, "ES384": 384, "ES512": 521}[algorithm]
		return expectedBits != 0 && publicKey.Params().BitSize == expectedBits
	case algorithm == "EdDSA":
		publicKey, ok := key.(ed25519.PublicKey)
		return ok && len(publicKey) == ed25519.PublicKeySize
	default:
		return false
	}
}

func validECDSAPublicKey(publicKey *ecdsa.PublicKey) (valid bool) {
	defer func() {
		if recover() != nil {
			valid = false
		}
	}()
	_, err := publicKey.Bytes()
	return err == nil
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
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
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
