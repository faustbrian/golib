package jwt_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
		if err := remote.Close(context.Background()); err != nil {
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

	state.set(marshalJWKSet(t, secondSet), http.StatusOK)
	if err := remote.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	secondToken := signedToken(t, secondSigner, jwa.RS256(), claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(second) error = %v", err)
	}

	state.set(nil, http.StatusServiceUnavailable)
	if err := remote.Refresh(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Refresh(outage) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(stale cached key) error = %v", err)
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
	t.Cleanup(func() { _ = remote.Close(context.Background()) })
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
	if _, err := authjwt.NewRemote(context.Background(), "https://user:password@example.test/keys"); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewRemote(userinfo) error = %v", err)
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

func TestRemoteLifecycleRejectsClosedAndCanceledOperations(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(marshalJWKSet(t, keys))
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
		authjwt.WithInitializationTimeout(time.Second),
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
	if err := remote.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := remote.Close(context.Background()); err != nil {
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
		if requests.Add(1) == 1 {
			_, _ = writer.Write(body)
			return
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
		authjwt.WithInitializationTimeout(25*time.Millisecond),
	)
	if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("NewRemote(timeout) = %v, %v", remote, err)
	}
}

func TestRemoteReportsCacheStartupCancellation(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key", jwa.RS256())
	body := marshalJWKSet(t, keys)
	lifecycle, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(&cancelOnEOFReader{reader: bytes.NewReader(body), cancel: cancel}),
			Request:    request,
		}, nil
	})}
	remote, err := authjwt.NewRemote(lifecycle, "https://issuer.example.test/keys",
		authjwt.WithHTTPClient(client), authjwt.WithInitializationTimeout(time.Second),
	)
	if remote != nil || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
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
	if err := remote.Close(context.Background()); err != nil {
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
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- remote.Close(ctx) }()
	select {
	case err := <-closeDone:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		releaseOnce.Do(func() { close(release) })
		<-refreshDone
		err := <-closeDone
		t.Fatalf("Close() ignored deadline while waiting for refresh: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	<-refreshDone
	if err := remote.Close(context.Background()); err != nil {
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
	_, err = validator.Authenticate(context.Background(), authentication.NewBearerCredential(header+"."+payload+".signature"))
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, providerError) {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if containsText(err.Error(), "secret-token") {
		t.Fatalf("Authenticate() disclosed provider error: %q", err)
	}
}

type jwkServerState struct {
	mutex  sync.RWMutex
	body   []byte
	status int
}

func (state *jwkServerState) set(body []byte, status int) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.body = append([]byte(nil), body...)
	state.status = status
}

func (state *jwkServerState) serveHTTP(writer http.ResponseWriter, _ *http.Request) {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type cancelOnEOFReader struct {
	reader *bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelOnEOFReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		r.cancel()
	}
	return count, err
}
