package jwt_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestRemoteJWKRotationAndIssuerOutage(t *testing.T) {
	t.Parallel()

	firstSet, firstSigner := rsaKeys(t, "first", jwa.RS256())
	secondSet, secondSigner := rsaKeys(t, "second", jwa.RS256())
	state := &jwkServerState{body: marshalJWKSet(t, firstSet)}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	t.Cleanup(server.Close)

	lifecycle, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	remote, err := authjwt.NewRemote(lifecycle, server.URL,
		authjwt.WithInsecureHTTP(),
		authjwt.WithHTTPClient(server.Client()),
		authjwt.WithRefreshBounds(10*time.Millisecond, time.Minute),
		authjwt.WithMaxJWKBodyBytes(32*1024),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	t.Cleanup(func() {
		if err := closeRemote(t, remote); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Provider: remote,
		Clock: authtest.NewClock(jwtNow),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	}
	firstToken := signedToken(t, firstSigner, jwa.RS256(), claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(firstToken)); err != nil {
		t.Fatalf("Authenticate(first) error = %v", err)
	}
	requestsBeforeUnknownKey := state.requests.Load()
	unknownToken := signedToken(t, secondSigner, jwa.RS256(), claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(unknownToken)); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(unknown key) error = %v", err)
	}
	if got := state.requests.Load(); got != requestsBeforeUnknownKey {
		t.Fatalf("unknown key triggered %d remote requests", got-requestsBeforeUnknownKey)
	}

	state.set(marshalJWKSet(t, secondSet), http.StatusOK)
	if err := remote.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	secondToken := signedToken(t, secondSigner, jwa.RS256(), claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(second) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(firstToken)); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(evicted key) error = %v", err)
	}

	state.set([]byte(`{"keys":[{"kid":"duplicate","kid":"duplicate"}]}`), http.StatusOK)
	if err := remote.Refresh(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(hostile response) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(after hostile refresh) error = %v", err)
	}

	state.set(nil, http.StatusServiceUnavailable)
	if err := remote.Refresh(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(outage) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(stale cached key) error = %v", err)
	}

	state.set(marshalJWKSet(t, firstSet), http.StatusOK)
	if err := remote.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh(recovery) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(firstToken)); err != nil {
		t.Fatalf("Authenticate(recovered key) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(recovery evicted stale key) error = %v", err)
	}
}

func TestRemoteRefreshAndAuthenticationAreRaceSafe(t *testing.T) {
	t.Parallel()

	firstSet, firstSigner := rsaKeys(t, "first", jwa.RS256())
	secondSet, secondSigner := rsaKeys(t, "second", jwa.RS256())
	state := &jwkServerState{body: marshalJWKSet(t, firstSet)}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
		authjwt.WithRefreshBounds(10*time.Millisecond, time.Minute),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	t.Cleanup(func() {
		if err := closeRemote(t, remote); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Provider: remote,
		Clock: authtest.NewClock(jwtNow),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	}
	tokens := []string{
		signedToken(t, firstSigner, jwa.RS256(), claims),
		signedToken(t, secondSigner, jwa.RS256(), claims),
	}
	var group sync.WaitGroup
	for index := range 4 {
		group.Add(1)
		go func(offset int) {
			defer group.Done()
			for attempt := range 50 {
				_, err := validator.ValidateBearer(context.Background(), tokens[(offset+attempt)%len(tokens)])
				if err != nil && !errors.Is(err, authentication.ErrCredentialsRejected) {
					t.Errorf("ValidateBearer() error = %v", err)
					return
				}
			}
		}(index)
	}
	for attempt := range 20 {
		body := marshalJWKSet(t, firstSet)
		if attempt%2 == 1 {
			body = marshalJWKSet(t, secondSet)
		}
		state.set(body, http.StatusOK)
		if err := remote.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
	}
	group.Wait()
}

func TestRemoteSerializesAutomaticAndExplicitRefreshWork(t *testing.T) {
	keys, _ := rsaKeys(t, "key", jwa.RS256())
	body := marshalJWKSet(t, keys)
	var requests atomic.Int64
	var active atomic.Int64
	started := make(chan struct{})
	overlap := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		if current > 1 {
			select {
			case overlap <- struct{}{}:
			default:
			}
		}
		if request >= 3 {
			if request == 3 {
				close(started)
			}
			<-release
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "max-age=0")
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
		authjwt.WithRefreshBounds(10*time.Millisecond, 10*time.Millisecond),
		authjwt.WithRefreshJitter(0),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	waitForRemoteSignal(t, started, "automatic refresh request")
	select {
	case <-overlap:
	default:
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- remote.Refresh(context.Background()) }()
	var overlapped bool
	select {
	case <-overlap:
		overlapped = true
	case <-time.After(200 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-refreshDone; err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if overlapped {
		t.Fatalf("automatic and explicit refresh performed overlapping remote work after %d requests", requests.Load())
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRemoteDoesNotTransferCachedKeyOwnership(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	t.Cleanup(func() { _ = closeRemote(t, remote) })

	first, err := remote.KeySet(context.Background())
	if err != nil {
		t.Fatalf("KeySet(first) error = %v", err)
	}
	key, ok := first.Key(0)
	if !ok {
		t.Fatal("KeySet(first) is empty")
	}
	if err := key.Set(jwk.KeyIDKey, "poisoned"); err != nil {
		t.Fatalf("Set(kid) error = %v", err)
	}
	if err := first.RemoveKey(key); err != nil {
		t.Fatalf("RemoveKey() error = %v", err)
	}

	second, err := remote.KeySet(context.Background())
	if err != nil {
		t.Fatalf("KeySet(second) error = %v", err)
	}
	if second.Len() != 1 {
		t.Fatalf("KeySet(second).Len() = %d, want 1", second.Len())
	}
	if _, ok := second.LookupKeyID("key"); !ok {
		t.Fatal("KeySet(second) lost the cached key identity")
	}
}

func TestRemoteBoundsConfigurationAndLifecycle(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	encodedKeys := marshalJWKSet(t, keys)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(encodedKeys)
	}))
	t.Cleanup(server.Close)

	if _, err := authjwt.NewRemote(context.Background(), server.URL); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewRemote(http) error = %v", err)
	}
	userinfoRequested := false
	userinfoClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		userinfoRequested = true
		return nil, errors.New("userinfo URL reached transport")
	})}
	if _, err := authjwt.NewRemote(
		context.Background(),
		"https://user:password@example.test/keys",
		authjwt.WithHTTPClient(userinfoClient),
	); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewRemote(userinfo) error = %v", err)
	}
	if userinfoRequested {
		t.Fatal("NewRemote(userinfo) made an HTTP request")
	}
	if _, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
		authjwt.WithMaxJWKBodyBytes(4),
	); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("NewRemote(oversized response) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authjwt.NewRemote(ctx, server.URL, authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client())); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewRemote(canceled) error = %v", err)
	}
}

func TestRemoteRejectsHostileJWKResponsesAtInitialization(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	valid := marshalJWKSet(t, keys)
	key, ok := keys.Key(0)
	if !ok {
		t.Fatal("RSA key set is empty")
	}
	encodedKey, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("json.Marshal(JWK) error = %v", err)
	}
	duplicateKeyID := []byte(`{"keys":[` + string(encodedKey) + `,` + string(encodedKey) + `]}`)
	duplicateSetMember := []byte(`{"keys":[],"keys":[` + string(encodedKey) + `]}`)
	invalidSurrogate := bytes.Replace(encodedKey, []byte(`"kid":"key"`), []byte(`"kid":"key","label":"\uD800"`), 1)
	invalidSurrogate = []byte(`{"keys":[` + string(invalidSurrogate) + `]}`)

	manyKeys := make([]map[string]any, 65)
	for index := range manyKeys {
		manyKeys[index] = map[string]any{
			"kty": "oct", "kid": fmt.Sprintf("key-%d", index), "alg": "HS256",
			"k": base64.RawURLEncoding.EncodeToString([]byte("01234567890123456789012345678901")),
		}
	}
	tooManyKeys, err := json.Marshal(map[string]any{"keys": manyKeys})
	if err != nil {
		t.Fatalf("json.Marshal(many keys) error = %v", err)
	}

	tests := map[string]struct {
		body   []byte
		header http.Header
	}{
		"duplicate key ID":  {body: duplicateKeyID},
		"duplicate member":  {body: duplicateSetMember},
		"invalid surrogate": {body: invalidSurrogate},
		"too many keys":     {body: tooManyKeys},
		"oversized headers": {body: valid, header: http.Header{"X-Oversized": {strings.Repeat("x", 64*1024)}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				for header, values := range tt.header {
					for _, value := range values {
						writer.Header().Add(header, value)
					}
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write(tt.body)
			}))
			t.Cleanup(server.Close)
			remote, err := authjwt.NewRemote(context.Background(), server.URL,
				authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
			)
			if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
				if remote != nil {
					_ = closeRemote(t, remote)
				}
				t.Fatalf("NewRemote() = %v, %v; want unavailable", remote, err)
			}
		})
	}
}

func TestRemoteClassifiesInjectedNetworkAndBodyFailures(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	valid := marshalJWKSet(t, keys)
	tests := map[string]struct {
		transport http.RoundTripper
		timeout   time.Duration
	}{
		"DNS": {transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{Err: "injected", Name: "issuer.example.test"}
		})},
		"TLS": {transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, tls.RecordHeaderError{Msg: "injected handshake failure"}
		})},
		"timeout": {timeout: 10 * time.Millisecond, transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})},
		"partial body": {transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(io.MultiReader(bytes.NewReader(valid[:len(valid)/2]), failingReader{})), Request: request,
			}, nil
		})},
		"compressed body": {transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}},
				Body:       io.NopCloser(bytes.NewReader(valid)), Request: request,
			}, nil
		})},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			timeout := tt.timeout
			if timeout == 0 {
				timeout = time.Second
			}
			remote, err := authjwt.NewRemote(
				context.Background(), "https://issuer.example.test/keys",
				authjwt.WithHTTPClient(&http.Client{Transport: tt.transport}),
				authjwt.WithInitializationTimeout(timeout),
			)
			if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
				t.Fatalf("NewRemote() = %v, %v; want unavailable", remote, err)
			}
		})
	}
}

func TestRemoteRejectsRedirects(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	remote, err := authjwt.NewRemote(context.Background(), redirect.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(redirect.Client()),
	)
	if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("NewRemote(redirect) = %v, %v; want unavailable", remote, err)
	}
}

func TestRemoteLifecycleRejectsClosedAndCanceledOperations(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	if _, err := remote.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := remote.KeySet(canceled); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(canceled) error = %v", err)
	}
	if err := remote.Refresh(canceled); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(canceled) error = %v", err)
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
	if _, err := remote.KeySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(closed) error = %v", err)
	}
	if err := remote.Refresh(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(closed) error = %v", err)
	}
}

func TestRemoteRegistrationIsInitializationBounded(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	body := marshalJWKSet(t, keys)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(),
		authjwt.WithInitializationTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("NewRemote() requests = %d, want 1", got)
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRemoteReportsCacheStartupCancellation(t *testing.T) {
	lifecycle, cancel := context.WithTimeout(context.Background(), time.Second)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(cancelOnReadReader{cancel: cancel}),
			Request:    request,
		}, nil
	})}
	remote, err := authjwt.NewRemote(lifecycle, "https://issuer.example.test/keys",
		authjwt.WithHTTPClient(client), authjwt.WithInitializationTimeout(time.Second),
	)
	if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("NewRemote(canceled startup) = %v, %v", remote, err)
	}
}

func TestRemoteCloseReportsCanceledJoin(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := remote.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close(canceled) error = %v", err)
	}
	if _, err := remote.KeySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(after canceled close) error = %v", err)
	}
	if err := remote.Refresh(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(after canceled close) error = %v", err)
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close(cleanup) error = %v", err)
	}
}

func TestRemoteCloseDeadlineIsNotBlockedByRefreshLock(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	body := marshalJWKSet(t, keys)
	var block atomic.Bool
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if block.Load() {
			startedOnce.Do(func() { close(started) })
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
		}
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	block.Store(true)
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		_ = remote.Refresh(context.Background())
	}()
	waitForRemoteSignal(t, started, "blocked refresh request")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	closeDone := make(chan error, 1)
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closeDone <- remote.Close(ctx)
	}()
	waitForRemoteSignal(t, closeStarted, "close call")
	select {
	case err := <-closeDone:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		releaseOnce.Do(func() { close(release) })
		waitForRemoteSignal(t, refreshDone, "refresh cleanup")
		err := <-closeDone
		t.Fatalf("Close() ignored deadline while waiting for refresh: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	waitForRemoteSignal(t, refreshDone, "refresh completion")
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close(cleanup) error = %v", err)
	}
}

func TestProviderFailureIsUnavailableAndSecretSafe(t *testing.T) {
	t.Parallel()

	providerError := errors.New("provider failed while handling secret-token")
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
		Provider: authjwt.KeyProviderFunc(func(context.Context) (jwk.Set, error) {
			return nil, providerError
		}),
		Clock: authtest.NewClock(jwtNow),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"service"}`))
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	_, err = validator.Authenticate(context.Background(), authentication.NewBearerCredential(header+"."+payload+"."+signature))
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, authjwt.ErrKeyProviderUnavailable) {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if errors.Is(err, providerError) {
		t.Fatal("Authenticate() exposed the provider error through unwrapping")
	}
	if containsText(err.Error(), "secret-token") {
		t.Fatalf("Authenticate() disclosed provider error: %q", err)
	}
}

func TestRemoteFailureRedactsEndpointQueryAndTransportError(t *testing.T) {
	t.Parallel()

	remoteError := errors.New("issuer request failed with secret-response")
	_, err := authjwt.NewRemote(
		context.Background(),
		"https://issuer.example.test/keys?access_token=secret-query",
		authjwt.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, remoteError
		})}),
	)
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, authjwt.ErrKeyProviderUnavailable) {
		t.Fatalf("NewRemote() error = %v", err)
	}
	if errors.Is(err, remoteError) || strings.Contains(err.Error(), "secret-query") || strings.Contains(err.Error(), "secret-response") {
		t.Fatalf("NewRemote() exposed remote details: %v", err)
	}
}

func TestRemoteLifetimeIsOwnedByClose(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(server.Close)
	constructorContext, cancel := context.WithCancel(context.Background())
	remote, err := authjwt.NewRemote(constructorContext, server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewRemote() error = %v", err)
	}
	cancel()
	if _, err := remote.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet(after constructor cancellation) error = %v", err)
	}
	if err := closeRemote(t, remote); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func closeRemote(t *testing.T, remote *authjwt.Remote) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return remote.Close(ctx)
}

type jwkServerState struct {
	mutex    sync.RWMutex
	body     []byte
	status   int
	requests atomic.Int64
}

func (state *jwkServerState) set(body []byte, status int) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.body = append([]byte(nil), body...)
	state.status = status
}

func (state *jwkServerState) serveHTTP(writer http.ResponseWriter, _ *http.Request) {
	state.requests.Add(1)
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	status := state.status
	if status == 0 {
		status = http.StatusOK
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(state.body)
}

func marshalJWKSet(t *testing.T, set jwk.Set) []byte {
	t.Helper()
	encoded, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("json.Marshal(JWK set) error = %v", err)
	}
	return encoded
}

func containsText(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func waitForRemoteSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("injected partial-body failure") }

type cancelOnReadReader struct {
	cancel context.CancelFunc
}

func (reader cancelOnReadReader) Read([]byte) (int, error) {
	reader.cancel()
	return 0, context.Canceled
}
