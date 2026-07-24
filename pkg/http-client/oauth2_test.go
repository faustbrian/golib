package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestOAuth2AuthIntegratesReusableTokenSource(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	source := oauth2.TokenSource(oauth2TokenSourceFunc(func() (*oauth2.Token, error) {
		calls.Add(1)

		return &oauth2.Token{
			AccessToken: "access-token",
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(time.Hour),
		}, nil
	}))
	editor, err := NewOAuth2Auth(source)
	if err != nil {
		t.Fatalf("construct OAuth2 editor: %v", err)
	}

	for range 2 {
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		if err := editor.EditRequest(request); err != nil {
			t.Fatalf("edit request: %v", err)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token source calls = %d, want 1", calls.Load())
	}
}

func TestContextOAuth2AuthUsesRequestContext(t *testing.T) {
	t.Parallel()

	contextKey := oauth2ContextKey{}
	source := ContextTokenSourceFunc(func(ctx context.Context) (*oauth2.Token, error) {
		if got := ctx.Value(contextKey); got != "request-value" {
			t.Fatalf("context value = %v", got)
		}

		return &oauth2.Token{AccessToken: "access-token", TokenType: "bearer"}, nil
	})
	editor, err := NewContextOAuth2Auth(source)
	if err != nil {
		t.Fatalf("construct OAuth2 editor: %v", err)
	}
	request, err := http.NewRequestWithContext(
		context.WithValue(context.Background(), contextKey, "request-value"),
		http.MethodGet,
		"https://api.example.test",
		nil,
	)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	if err := editor.EditRequest(request); err != nil {
		t.Fatalf("edit request: %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestOAuth2AuthErrorsAreTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("token refresh failed with do-not-render")
	tests := []struct {
		name   string
		source ContextTokenSource
		cause  error
	}{
		{
			name: "source failure",
			source: ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
				return nil, secretCause
			}),
			cause: secretCause,
		},
		{
			name: "nil token",
			source: ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
				return nil, nil
			}),
			cause: ErrInvalidOAuth2Token,
		},
		{
			name: "empty access token",
			source: ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
				return &oauth2.Token{}, nil
			}),
			cause: ErrInvalidOAuth2Token,
		},
		{
			name: "unsafe token type",
			source: ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
				return &oauth2.Token{AccessToken: "do-not-render", TokenType: "Bearer\r\nInjected"}, nil
			}),
			cause: ErrInvalidOAuth2Token,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			editor, err := NewContextOAuth2Auth(test.source)
			if err != nil {
				t.Fatalf("construct OAuth2 editor: %v", err)
			}
			request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			err = editor.EditRequest(request)
			var tokenError *OAuth2TokenError
			if !errors.As(err, &tokenError) || !errors.Is(err, test.cause) {
				t.Fatalf("OAuth2 error = %#v", err)
			}
			if strings.Contains(err.Error(), "do-not-render") {
				t.Fatalf("OAuth2 error rendered secret: %q", err)
			}
		})
	}
}

func TestClientCredentialsTokenSourceCoordinatesRefreshAndBypassesMiddleware(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int64
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		tokenRequests.Add(1)
		username, password, ok := request.BasicAuth()
		if !ok || username != "client-id" || password != "client-secret" {
			t.Errorf("unexpected client authentication: %q %q %v", username, password, ok)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		if request.Form.Get("grant_type") != "client_credentials" ||
			request.Form.Get("scope") != "read write" ||
			request.Form.Get("audience") != "vendor-api" {
			t.Errorf("unexpected token form: %v", request.Form)
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"access_token": "coordinated-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}); err != nil {
			t.Errorf("encode token response: %v", err)
		}
	}))
	defer tokenServer.Close()

	var middlewareCalls atomic.Int64
	observer := mustCompletionMiddleware(t, MiddlewareOptions{
		Name: "observe-token-recursion", Scope: ScopeOperation, Layer: MiddlewareClient,
	}, func(*http.Request, *http.Response, error) error {
		middlewareCalls.Add(1)

		return nil
	})
	client, err := New(Config{
		Transport:  tokenServer.Client().Transport,
		Middleware: []Middleware{observer},
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	scopes := []string{"read", "write"}
	parameters := url.Values{"audience": {"vendor-api"}}
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client:         client,
		TokenURL:       tokenServer.URL,
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		Scopes:         scopes,
		EndpointParams: parameters,
	})
	if err != nil {
		t.Fatalf("construct token source: %v", err)
	}
	scopes[0] = "mutated"
	parameters.Set("audience", "mutated")

	const callers = 32
	start := make(chan struct{})
	tokens := make([]*oauth2.Token, callers)
	errorsByCaller := make([]error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := range callers {
		go func() {
			defer wait.Done()
			<-start
			tokens[index], errorsByCaller[index] = source.Token(context.Background())
		}()
	}
	close(start)
	wait.Wait()
	for index, err := range errorsByCaller {
		if err != nil {
			t.Fatalf("caller %d token error: %v", index, err)
		}
		if tokens[index].AccessToken != "coordinated-token" {
			t.Fatalf("caller %d token = %#v", index, tokens[index])
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests.Load())
	}
	if middlewareCalls.Load() != 0 {
		t.Fatalf("integration middleware calls = %d, want 0", middlewareCalls.Load())
	}
	tokens[0].AccessToken = "mutated"
	if tokens[1].AccessToken != "coordinated-token" {
		t.Fatal("callers received aliased token pointers")
	}
}

func TestClientCredentialsTokenSourceCancelsRefreshWaiters(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	client, err := New(Config{Transport: tokenServer.Client().Transport})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client: client, TokenURL: tokenServer.URL, ClientID: "id", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatalf("construct token source: %v", err)
	}

	leaderResult := make(chan error, 1)
	go func() {
		_, tokenErr := source.Token(context.Background())
		leaderResult <- tokenErr
	}()
	<-entered
	underlying, cancel := context.WithCancel(context.Background())
	canceled := &observedDoneContext{Context: underlying, observed: make(chan struct{})}
	waiterResult := make(chan error, 1)
	go func() {
		_, tokenErr := source.Token(canceled)
		waiterResult <- tokenErr
	}()
	<-canceled.observed
	started := time.Now()
	cancel()
	err = <-waiterResult
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("canceled waiter took %s", elapsed)
	}
	close(release)
	if err := <-leaderResult; err != nil {
		t.Fatalf("leader refresh: %v", err)
	}
}

func TestClientCredentialsTokenRequestHonorsCallerAndClientCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		cancelWith func(*testing.T, context.CancelFunc, *Client)
		want       error
	}{
		{
			name: "caller deadline",
			cancelWith: func(t *testing.T, cancel context.CancelFunc, _ *Client) {
				t.Helper()
				cancel()
			},
			want: context.Canceled,
		},
		{
			name: "client close",
			cancelWith: func(t *testing.T, _ context.CancelFunc, client *Client) {
				t.Helper()
				if err := client.Close(); err != nil {
					t.Fatalf("close client: %v", err)
				}
			},
			want: ErrClientClosed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				close(entered)
				select {
				case <-request.Context().Done():
				case <-release:
				}
			}))
			defer tokenServer.Close()
			defer tokenServer.CloseClientConnections()
			client, err := New(Config{Transport: tokenServer.Client().Transport})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close client: %v", err)
				}
			}()
			source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
				Client: client, TokenURL: tokenServer.URL, ClientID: "id", ClientSecret: "secret",
			})
			if err != nil {
				t.Fatalf("construct token source: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, tokenErr := source.Token(ctx)
				result <- tokenErr
			}()
			<-entered
			test.cancelWith(t, cancel, client)
			select {
			case err := <-result:
				if !errors.Is(err, test.want) {
					t.Fatalf("token cancellation error = %v, want %v", err, test.want)
				}
			case <-time.After(time.Second):
				t.Fatal("token request did not stop after cancellation")
			}
			close(release)
		})
	}
}

func TestClientCredentialsTokenSourceRejectsCachedTokenAfterClientClose(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client: client, TokenURL: "https://tokens.example.test", ClientID: "id", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatalf("construct token source: %v", err)
	}
	source.token = &oauth2.Token{
		AccessToken: "cached", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour),
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	_, err = source.Token(context.Background())
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("token after client close error = %v", err)
	}
}

func TestOAuth2AuthBoundaryContracts(t *testing.T) {
	t.Parallel()

	var typedNilSource *oauth2NilTokenSource
	if _, err := NewOAuth2Auth(typedNilSource); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("typed-nil OAuth2 source error = %v", err)
	}
	var typedNilContextSource *oauth2NilContextTokenSource
	if _, err := NewContextOAuth2Auth(typedNilContextSource); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("typed-nil context source error = %v", err)
	}

	validSource := ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "token", TokenType: "Bearer"}, nil
	})
	editor, err := NewContextOAuth2Auth(validSource)
	if err != nil {
		t.Fatalf("construct editor: %v", err)
	}
	if err := editor.EditRequest(nil); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("nil request error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(canceled, http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct canceled request: %v", err)
	}
	if err := editor.EditRequest(request); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled request error = %v", err)
	}

	ctx, cancelAfterSource := context.WithCancel(context.Background())
	editor, err = NewContextOAuth2Auth(ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
		cancelAfterSource()

		return &oauth2.Token{AccessToken: "token", TokenType: "Bearer"}, nil
	}))
	if err != nil {
		t.Fatalf("construct canceling editor: %v", err)
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	if err := editor.EditRequest(request); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-source cancellation error = %v", err)
	}

	for _, token := range []*oauth2.Token{
		{AccessToken: "expired", Expiry: time.Now().Add(-time.Hour)},
		{AccessToken: "unsafe\r\ndo-not-render", TokenType: "Bearer"},
	} {
		editor, err := NewContextOAuth2Auth(ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
			return token, nil
		}))
		if err != nil {
			t.Fatalf("construct invalid-token editor: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		err = editor.EditRequest(request)
		if !errors.Is(err, ErrInvalidOAuth2Token) || strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("invalid token error = %v", err)
		}
	}
}

func TestClientCredentialsRejectsInvalidAndMutablePolicy(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	valid := ClientCredentialsOptions{
		Client: client, TokenURL: "https://tokens.example.test/token", ClientID: "id", ClientSecret: "secret",
	}
	tests := []struct {
		name   string
		mutate func(*ClientCredentialsOptions)
	}{
		{name: "nil client", mutate: func(options *ClientCredentialsOptions) { options.Client = nil }},
		{name: "empty ID", mutate: func(options *ClientCredentialsOptions) { options.ClientID = "" }},
		{name: "empty secret", mutate: func(options *ClientCredentialsOptions) { options.ClientSecret = "" }},
		{name: "relative URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "/token" }},
		{name: "userinfo URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "https://user:secret@tokens.example.test" }},
		{name: "query URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "https://tokens.example.test?secret=value" }},
		{name: "fragment URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "https://tokens.example.test#fragment" }},
		{name: "insecure URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "http://tokens.example.test" }},
		{name: "malformed URL", mutate: func(options *ClientCredentialsOptions) { options.TokenURL = "%" }},
		{name: "negative early expiry", mutate: func(options *ClientCredentialsOptions) { options.EarlyExpiry = -time.Second }},
		{name: "unknown auth style", mutate: func(options *ClientCredentialsOptions) { options.AuthStyle = oauth2.AuthStyle(99) }},
		{name: "empty scope", mutate: func(options *ClientCredentialsOptions) { options.Scopes = []string{""} }},
		{name: "space in scope", mutate: func(options *ClientCredentialsOptions) { options.Scopes = []string{"read write"} }},
		{name: "quote in scope", mutate: func(options *ClientCredentialsOptions) { options.Scopes = []string{`read"write`} }},
		{name: "backslash in scope", mutate: func(options *ClientCredentialsOptions) { options.Scopes = []string{`read\write`} }},
		{name: "Unicode scope", mutate: func(options *ClientCredentialsOptions) { options.Scopes = []string{"réad"} }},
		{name: "reserved client ID", mutate: func(options *ClientCredentialsOptions) { options.EndpointParams = url.Values{"client_id": {"other"}} }},
		{name: "reserved secret", mutate: func(options *ClientCredentialsOptions) {
			options.EndpointParams = url.Values{"client_secret": {"other"}}
		}},
		{name: "reserved grant", mutate: func(options *ClientCredentialsOptions) { options.EndpointParams = url.Values{"grant_type": {"other"}} }},
		{name: "reserved scope", mutate: func(options *ClientCredentialsOptions) { options.EndpointParams = url.Values{"scope": {"other"}} }},
		{name: "empty parameter", mutate: func(options *ClientCredentialsOptions) { options.EndpointParams = url.Values{"": {"other"}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			_, err := NewClientCredentialsTokenSource(options)
			if !errors.Is(err, ErrInvalidAuthentication) {
				t.Fatalf("invalid policy error = %v", err)
			}
			if strings.Contains(err.Error(), "secret=value") {
				t.Fatalf("policy error rendered URL query: %q", err)
			}
		})
	}

	closedClient, err := New(Config{})
	if err != nil {
		t.Fatalf("construct closed client: %v", err)
	}
	if err := closedClient.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	closedOptions := valid
	closedOptions.Client = closedClient
	if _, err := NewClientCredentialsTokenSource(closedOptions); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("closed client policy error = %v", err)
	}

	fixedNow := time.Unix(1_700_000_000, 0)
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client: client, TokenURL: "http://tokens.example.test/token", ClientID: "id", ClientSecret: "secret",
		AllowInsecureURL: true,
		AuthStyle:        oauth2.AuthStyleInParams,
		EarlyExpiry:      time.Minute,
		Now:              func() time.Time { return fixedNow },
		Scopes:           []string{"read", "admin:write"},
		EndpointParams:   url.Values{"audience": {"vendor"}},
	})
	if err != nil {
		t.Fatalf("construct explicit policy: %v", err)
	}
	if !source.now().Equal(fixedNow) || source.earlyExpiry != time.Minute || source.config.AuthStyle != oauth2.AuthStyleInParams {
		t.Fatalf("explicit policy not retained: %#v", source)
	}
	var nilContext context.Context
	if _, err := source.Token(nilContext); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatalf("nil context error = %v", err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Token(canceledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled context error = %v", err)
	}
}

func TestClientCredentialsTokenSourceSupportsExplicitParameterAuthentication(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, _, ok := request.BasicAuth(); ok {
			t.Error("parameter authentication unexpectedly sent Basic credentials")
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		if request.Form.Get("client_id") != "id" || request.Form.Get("client_secret") != "secret" {
			t.Errorf("parameter credentials = %v", request.Form)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client: client, TokenURL: tokenServer.URL, ClientID: "id", ClientSecret: "secret",
		AllowInsecureURL: true, AuthStyle: oauth2.AuthStyleInParams,
	})
	if err != nil {
		t.Fatalf("construct token source: %v", err)
	}
	token, err := source.Token(context.Background())
	if err != nil || token.AccessToken != "token" {
		t.Fatalf("parameter token = %#v, %v", token, err)
	}
}

func TestClientCredentialsEndpointFailuresAreTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		response   string
		wantCause  error
		credential string
	}{
		{
			name: "HTTP failure", status: http.StatusUnauthorized,
			response:   `{"error":"invalid_client","error_description":"do-not-render"}`,
			credential: "do-not-render",
		},
		{
			name: "immediately expiring token", status: http.StatusOK,
			response:  `{"access_token":"short","token_type":"Bearer","expires_in":1}`,
			wantCause: ErrInvalidOAuth2Token,
		},
		{
			name: "unsafe access token", status: http.StatusOK,
			response:  `{"access_token":"unsafe\r\ndo-not-render","token_type":"Bearer","expires_in":3600}`,
			wantCause: ErrInvalidOAuth2Token, credential: "do-not-render",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.response))
			}))
			defer tokenServer.Close()
			client, err := New(Config{Transport: tokenServer.Client().Transport})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close client: %v", err)
				}
			}()
			source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
				Client: client, TokenURL: tokenServer.URL, ClientID: "id", ClientSecret: "secret",
			})
			if err != nil {
				t.Fatalf("construct token source: %v", err)
			}
			_, err = source.Token(context.Background())
			var credentialsError *ClientCredentialsError
			if !errors.As(err, &credentialsError) {
				t.Fatalf("endpoint error = %#v", err)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("endpoint error = %v, want cause %v", err, test.wantCause)
			}
			if test.credential != "" && strings.Contains(err.Error(), test.credential) {
				t.Fatalf("endpoint error rendered credential: %q", err)
			}
		})
	}
}

func TestClientCredentialsWaiterStopsWhenClientCloses(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer tokenServer.Close()
	client, err := New(Config{Transport: tokenServer.Client().Transport})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	source, err := NewClientCredentialsTokenSource(ClientCredentialsOptions{
		Client: client, TokenURL: tokenServer.URL, ClientID: "id", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatalf("construct token source: %v", err)
	}
	leaderResult := make(chan error, 1)
	go func() {
		_, tokenErr := source.Token(context.Background())
		leaderResult <- tokenErr
	}()
	<-entered
	waiterContext := &observedDoneContext{Context: context.Background(), observed: make(chan struct{})}
	waiterResult := make(chan error, 1)
	go func() {
		_, tokenErr := source.Token(waiterContext)
		waiterResult <- tokenErr
	}()
	<-waiterContext.observed
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if err := <-waiterResult; !errors.Is(err, ErrClientClosed) {
		t.Fatalf("waiter close error = %v", err)
	}
	if err := <-leaderResult; !errors.Is(err, ErrClientClosed) {
		t.Fatalf("leader close error = %v", err)
	}
	close(release)
}

func TestOAuth2TokenHelpers(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	if validClientCredentialsToken(nil, now, 0) ||
		validClientCredentialsToken(&oauth2.Token{}, now, 0) ||
		validClientCredentialsToken(&oauth2.Token{AccessToken: "expired", Expiry: now}, now, 0) {
		t.Fatal("invalid token reported valid")
	}
	if !validClientCredentialsToken(&oauth2.Token{AccessToken: "no-expiry"}, now, 0) {
		t.Fatal("zero-expiry token reported invalid")
	}
	if cloneOAuth2Token(nil) != nil {
		t.Fatal("nil token clone was non-nil")
	}
}

type oauth2TokenSourceFunc func() (*oauth2.Token, error)

type oauth2ContextKey struct{}

func (function oauth2TokenSourceFunc) Token() (*oauth2.Token, error) {
	return function()
}

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

type oauth2NilTokenSource struct{}

func (*oauth2NilTokenSource) Token() (*oauth2.Token, error) { return nil, nil }

type oauth2NilContextTokenSource struct{}

func (*oauth2NilContextTokenSource) Token(context.Context) (*oauth2.Token, error) { return nil, nil }

func (ctx *observedDoneContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })

	return ctx.Context.Done()
}
