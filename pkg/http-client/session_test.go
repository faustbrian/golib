package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionCookieJarsAreOptInAndIsolatedPerClient(t *testing.T) {
	t.Parallel()

	plain, err := New(Config{})
	if err != nil {
		t.Fatalf("construct plain client: %v", err)
	}
	defer func() {
		if err := plain.Close(); err != nil {
			t.Errorf("close plain client: %v", err)
		}
	}()
	if plain.HTTPClient().Jar != nil {
		t.Fatal("zero configuration created an ambient cookie jar")
	}

	first, err := New(Config{Session: &SessionConfig{}})
	if err != nil {
		t.Fatalf("construct first session client: %v", err)
	}
	defer func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first client: %v", err)
		}
	}()
	second, err := New(Config{Session: &SessionConfig{}})
	if err != nil {
		t.Fatalf("construct second session client: %v", err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second client: %v", err)
		}
	}()
	if first.HTTPClient().Jar == nil || second.HTTPClient().Jar == nil {
		t.Fatal("opt-in session did not create cookie jars")
	}
	target, err := url.Parse("https://api.example.test/items")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	first.HTTPClient().Jar.SetCookies(target, []*http.Cookie{{Name: "session", Value: "first", Path: "/"}})
	if got := first.HTTPClient().Jar.Cookies(target); len(got) != 1 || got[0].Value != "first" {
		t.Fatalf("first client cookies = %#v", got)
	}
	if got := second.HTTPClient().Jar.Cookies(target); len(got) != 0 {
		t.Fatalf("cookie crossed client boundary: %#v", got)
	}

	publicTarget, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("parse public target: %v", err)
	}
	first.HTTPClient().Jar.SetCookies(publicTarget, []*http.Cookie{
		{Name: "public-suffix", Value: "rejected", Domain: "com", Path: "/"},
	})
	for _, cookie := range first.HTTPClient().Jar.Cookies(publicTarget) {
		if cookie.Name == "public-suffix" {
			t.Fatal("default jar accepted a public-suffix cookie")
		}
	}
}

func TestSessionRedirectPolicyControlsCrossOriginCookies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		policy     CookieRedirectPolicy
		redirect   string
		wantCookie string
	}{
		{
			name:       "same origin default",
			redirect:   "https://a.example.test/finish",
			wantCookie: "session=secret",
		},
		{
			name:     "cross origin stripped by default",
			redirect: "https://b.example.test/finish",
		},
		{
			name:       "jar scope explicitly allowed",
			policy:     CookieRedirectJar,
			redirect:   "https://b.example.test/finish",
			wantCookie: "session=secret",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			attempts := 0
			transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return &http.Response{
						StatusCode: http.StatusFound,
						Header: http.Header{
							"Location":   {test.redirect},
							"Set-Cookie": {"session=secret; Domain=example.test; Path=/"},
						},
						Body: io.NopCloser(http.NoBody),
					}, nil
				}
				if got := request.Header.Get("Cookie"); got != test.wantCookie {
					t.Fatalf("redirect cookie = %q, want %q", got, test.wantCookie)
				}

				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
			})
			client, err := New(Config{
				Transport: transport,
				Session:   &SessionConfig{RedirectPolicy: test.policy},
			})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close client: %v", err)
				}
			}()
			request, err := http.NewRequest(http.MethodGet, "https://a.example.test/start", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("execute request: %v", err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("close response: %v", err)
			}
			if attempts != 2 {
				t.Fatalf("attempts = %d, want 2", attempts)
			}
		})
	}
}

func TestSessionPersistenceStopsBeforeCanceledOrClosedWork(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	persistence := sessionPersistenceFunc{
		load: func(context.Context, http.CookieJar) error {
			calls.Add(1)

			return nil
		},
		save: func(context.Context, http.CookieJar) error {
			calls.Add(1)

			return nil
		},
	}
	client, err := New(Config{Session: &SessionConfig{Persistence: persistence}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.LoadSession(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load error = %v", err)
	}
	if err := client.SaveSession(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("persistence calls after canceled context = %d", calls.Load())
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if err := client.LoadSession(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("closed load error = %v", err)
	}
	if err := client.SaveSession(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("closed save error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("persistence calls after client close = %d", calls.Load())
	}
}

func TestSessionSetupFailureClosesOwnedJar(t *testing.T) {
	t.Parallel()

	jar := &trackingCookieJar{}
	_, err := New(Config{
		Session:    &SessionConfig{Jar: jar, JarOwnership: CookieJarOwned},
		Middleware: []Middleware{{}},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("setup error = %v", err)
	}
	if !jar.closed.Load() {
		t.Fatal("owned cookie jar remained open after setup failure")
	}
}

func TestSessionPersistenceLoadSaveAndJarOwnership(t *testing.T) {
	t.Parallel()

	target, err := url.Parse("https://api.example.test")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	baseJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("construct jar: %v", err)
	}
	ownedJar := &closableCookieJar{CookieJar: baseJar}
	var loads atomic.Int64
	var saves atomic.Int64
	persistence := sessionPersistenceFunc{
		load: func(ctx context.Context, jar http.CookieJar) error {
			loads.Add(1)
			if _, ok := ctx.Deadline(); !ok {
				t.Error("load context has no finite deadline")
			}
			jar.SetCookies(target, []*http.Cookie{{Name: "session", Value: "loaded", Path: "/"}})

			return nil
		},
		save: func(ctx context.Context, jar http.CookieJar) error {
			call := saves.Add(1)
			if _, ok := ctx.Deadline(); call == 2 && !ok {
				t.Error("save context has no finite deadline")
			}
			cookies := jar.Cookies(target)
			if len(cookies) != 1 || cookies[0].Value != "loaded" {
				t.Errorf("persisted cookies = %#v", cookies)
			}

			return nil
		},
	}
	client, err := New(Config{Session: &SessionConfig{
		Jar:                ownedJar,
		JarOwnership:       CookieJarOwned,
		Persistence:        persistence,
		LoadOnStart:        true,
		SaveOnClose:        true,
		PersistenceTimeout: time.Second,
	}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	if loads.Load() != 1 {
		t.Fatalf("load calls = %d, want 1", loads.Load())
	}
	if err := client.SaveSession(context.Background()); err != nil {
		t.Fatalf("manual save: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if saves.Load() != 2 {
		t.Fatalf("save calls = %d, want 2", saves.Load())
	}
	if !ownedJar.closed.Load() {
		t.Fatal("owned jar was not closed")
	}

	borrowedBase, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("construct borrowed jar: %v", err)
	}
	borrowedJar := &closableCookieJar{CookieJar: borrowedBase}
	borrowed, err := New(Config{Session: &SessionConfig{Jar: borrowedJar}})
	if err != nil {
		t.Fatalf("construct borrowed client: %v", err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatalf("close borrowed client: %v", err)
	}
	if borrowedJar.closed.Load() {
		t.Fatal("borrowed jar was closed")
	}
}

func TestSessionPersistenceFailuresAreBoundedAndSecretSafe(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("storage failure with cookie do-not-render")
	unknownOperation := &SessionPersistenceError{
		Operation: SessionPersistenceOperation(99),
		Cause:     secretCause,
	}
	if strings.Contains(unknownOperation.Error(), "do-not-render") ||
		unknownOperation.Error() != "HTTP session operation(99) failed" {
		t.Fatalf("unknown persistence operation error = %q", unknownOperation)
	}
	t.Run("load", func(t *testing.T) {
		jar := &trackingCookieJar{}
		persistence := sessionPersistenceFunc{
			load: func(context.Context, http.CookieJar) error { return secretCause },
			save: func(context.Context, http.CookieJar) error { return nil },
		}
		_, err := New(Config{Session: &SessionConfig{
			Jar: jar, JarOwnership: CookieJarOwned, Persistence: persistence, LoadOnStart: true,
		}})
		var persistenceError *SessionPersistenceError
		if !errors.As(err, &persistenceError) || !errors.Is(err, secretCause) {
			t.Fatalf("load error = %#v", err)
		}
		if strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("load error rendered secret: %q", err)
		}
		if !jar.closed.Load() {
			t.Fatal("load failure did not close owned jar")
		}
	})

	t.Run("save timeout", func(t *testing.T) {
		jar := &trackingCookieJar{}
		persistence := sessionPersistenceFunc{
			load: func(context.Context, http.CookieJar) error { return nil },
			save: func(ctx context.Context, _ http.CookieJar) error {
				<-ctx.Done()

				return ctx.Err()
			},
		}
		client, err := New(Config{Session: &SessionConfig{
			Jar: jar, JarOwnership: CookieJarOwned, Persistence: persistence,
			SaveOnClose: true, PersistenceTimeout: 20 * time.Millisecond,
		}})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		started := time.Now()
		err = client.Close()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("save timeout error = %v", err)
		}
		if strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("save error rendered secret: %q", err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("bounded save took %s", elapsed)
		}
		if !jar.closed.Load() {
			t.Fatal("save timeout did not close owned jar")
		}
	})

	t.Run("jar close", func(t *testing.T) {
		jar := &errorCookieJar{closeErr: secretCause}
		client, err := New(Config{Session: &SessionConfig{Jar: jar, JarOwnership: CookieJarOwned}})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		err = client.Close()
		var closeError *SessionCloseError
		if !errors.As(err, &closeError) || !errors.Is(err, secretCause) {
			t.Fatalf("close error = %#v", err)
		}
		if strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("close error rendered secret: %q", err)
		}
	})
}

func TestSessionMiddlewarePrecedesAuthenticationByDefault(t *testing.T) {
	t.Parallel()

	editor, err := NewBearerAuth("token")
	if err != nil {
		t.Fatalf("construct editor: %v", err)
	}
	authentication, err := NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "vendor-auth", Layer: MiddlewareClient,
	}, editor)
	if err != nil {
		t.Fatalf("construct authentication: %v", err)
	}
	client, err := New(Config{Session: &SessionConfig{}, Middleware: authentication})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	inspection := client.InspectPipeline()
	for index, plan := range [][]MiddlewareInfo{inspection.Operation, inspection.Attempt} {
		var requestNames []string
		for _, information := range plan {
			if information.Stage == StageRequest {
				requestNames = append(requestNames, information.Name)
			}
		}
		want := []string{"httpclient.session", "vendor-auth"}
		if index == 0 {
			want = []string{"httpclient.operation-identity", "httpclient.session", "vendor-auth"}
		}
		if len(requestNames) != len(want) {
			t.Fatalf("request middleware order = %v, want %v", requestNames, want)
		}
		for position := range want {
			if requestNames[position] != want[position] {
				t.Fatalf("request middleware order = %v, want %v", requestNames, want)
			}
		}
	}
}

func TestSessionRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedNilJar *trackingCookieJar
	var typedNilPersistence *sessionPointerPersistence
	tests := []SessionConfig{
		{JarOwnership: CookieJarOwnership(99)},
		{RedirectPolicy: CookieRedirectPolicy(99)},
		{PersistenceTimeout: -time.Second},
		{LoadOnStart: true},
		{SaveOnClose: true},
		{Jar: &trackingCookieJar{}, PublicSuffixList: testPublicSuffixList{}},
		{Jar: typedNilJar},
		{LoadOnStart: true, Persistence: typedNilPersistence},
	}
	for _, config := range tests {
		_, err := New(Config{Session: &config})
		if !errors.Is(err, ErrInvalidConfig) || !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("invalid session %#v error = %v", config, err)
		}
	}
}

func TestSessionManualPersistenceBoundaryContracts(t *testing.T) {
	t.Parallel()

	plain, err := New(Config{})
	if err != nil {
		t.Fatalf("construct plain client: %v", err)
	}
	defer func() {
		if err := plain.Close(); err != nil {
			t.Errorf("close plain client: %v", err)
		}
	}()
	if err := plain.LoadSession(context.Background()); !errors.Is(err, ErrSessionDisabled) {
		t.Fatalf("disabled load error = %v", err)
	}
	if err := plain.SaveSession(context.Background()); !errors.Is(err, ErrSessionDisabled) {
		t.Fatalf("disabled save error = %v", err)
	}

	session, err := New(Config{Session: &SessionConfig{}})
	if err != nil {
		t.Fatalf("construct session client: %v", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Errorf("close session client: %v", err)
		}
	}()
	var nilContext context.Context
	if err := session.LoadSession(nilContext); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("nil load context error = %v", err)
	}
	if err := session.SaveSession(nilContext); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("nil save context error = %v", err)
	}
	if err := session.LoadSession(context.Background()); !errors.Is(err, ErrSessionPersistenceUnavailable) {
		t.Fatalf("missing load persistence error = %v", err)
	}
	if err := session.SaveSession(context.Background()); !errors.Is(err, ErrSessionPersistenceUnavailable) {
		t.Fatalf("missing save persistence error = %v", err)
	}
}

func TestSessionPersistenceReturnsClosedWhenClientClosesDuringCall(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"load", "save"} {
		t.Run(operation, func(t *testing.T) {
			var client *Client
			persistence := sessionPersistenceFunc{
				load: func(context.Context, http.CookieJar) error {
					return client.Close()
				},
				save: func(context.Context, http.CookieJar) error {
					return client.Close()
				},
			}
			var err error
			client, err = New(Config{Session: &SessionConfig{Persistence: persistence}})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			if operation == "load" {
				err = client.LoadSession(context.Background())
			} else {
				err = client.SaveSession(context.Background())
			}
			if !errors.Is(err, ErrClientClosed) {
				t.Fatalf("%s error = %v", operation, err)
			}
		})
	}
}

func TestSessionMiddlewareRejectsMissingContextAndInvalidOrigins(t *testing.T) {
	t.Parallel()

	middleware := newSessionMiddleware(CookieRedirectSameOrigin)
	attemptOnly, err := NewPipeline(middleware[1])
	if err != nil {
		t.Fatalf("construct attempt pipeline: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	_, err = attemptOnly.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("missing session context error = %v", err)
	}

	pipeline, err := NewPipeline(middleware...)
	if err != nil {
		t.Fatalf("construct session pipeline: %v", err)
	}
	for _, invalid := range []*http.Request{
		{Method: http.MethodGet, Header: make(http.Header)},
		{Method: http.MethodGet, URL: &url.URL{Scheme: "ftp", Host: "api.example.test"}, Header: make(http.Header)},
	} {
		_, err := pipeline.Execute(invalid, TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}))
		if !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("invalid origin error = %v", err)
		}
	}

	breakAttemptURL := mustRequestMiddleware(t, MiddlewareOptions{
		Name: "break-session-url", Scope: ScopeOperation, Layer: MiddlewareOneShot,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		request.URL = nil

		return next(request)
	})
	pipeline, err = NewPipeline(append(middleware, breakAttemptURL)...)
	if err != nil {
		t.Fatalf("construct URL-breaking pipeline: %v", err)
	}
	request, err = http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	_, err = pipeline.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("invalid attempt origin error = %v", err)
	}
}

type sessionPersistenceFunc struct {
	load func(context.Context, http.CookieJar) error
	save func(context.Context, http.CookieJar) error
}

type sessionPointerPersistence struct{}

func (*sessionPointerPersistence) Load(context.Context, http.CookieJar) error { return nil }

func (*sessionPointerPersistence) Save(context.Context, http.CookieJar) error { return nil }

type testPublicSuffixList struct{}

func (testPublicSuffixList) PublicSuffix(domain string) string { return domain }

func (testPublicSuffixList) String() string { return "test" }

type trackingCookieJar struct {
	closed atomic.Bool
}

type closableCookieJar struct {
	http.CookieJar
	closed atomic.Bool
}

func (jar *closableCookieJar) Close() error {
	jar.closed.Store(true)

	return nil
}

type errorCookieJar struct {
	closeErr error
}

func (*errorCookieJar) SetCookies(*url.URL, []*http.Cookie) {}

func (*errorCookieJar) Cookies(*url.URL) []*http.Cookie { return nil }

func (jar *errorCookieJar) Close() error { return jar.closeErr }

func (*trackingCookieJar) SetCookies(*url.URL, []*http.Cookie) {}

func (*trackingCookieJar) Cookies(*url.URL) []*http.Cookie { return nil }

func (jar *trackingCookieJar) Close() error {
	jar.closed.Store(true)

	return nil
}

func (persistence sessionPersistenceFunc) Load(ctx context.Context, jar http.CookieJar) error {
	return persistence.load(ctx, jar)
}

func (persistence sessionPersistenceFunc) Save(ctx context.Context, jar http.CookieJar) error {
	return persistence.save(ctx, jar)
}
