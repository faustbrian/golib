package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

var (
	// ErrInvalidOAuth2Token indicates a missing, expired, or unsafe token.
	ErrInvalidOAuth2Token = errors.New("invalid OAuth2 token")
)

const defaultOAuth2EarlyExpiry = 10 * time.Second

// ContextTokenSource obtains an OAuth2 token using the request context.
// Implementations must coordinate concurrent refreshes and honor cancellation.
type ContextTokenSource interface {
	Token(context.Context) (*oauth2.Token, error)
}

// ContextTokenSourceFunc adapts a function to ContextTokenSource.
type ContextTokenSourceFunc func(context.Context) (*oauth2.Token, error)

// Token implements ContextTokenSource.
func (function ContextTokenSourceFunc) Token(ctx context.Context) (*oauth2.Token, error) {
	return function(ctx)
}

// OAuth2TokenError reports token acquisition or validation failure without
// rendering its cause, which may include credentials or endpoint data.
type OAuth2TokenError struct {
	Cause error
}

// ClientCredentialsOptions configures an outbound OAuth2 client-credentials
// source. Client supplies the hardened transport and finite total timeout.
type ClientCredentialsOptions struct {
	Client           *Client
	TokenURL         string
	ClientID         string
	ClientSecret     string
	Scopes           []string
	EndpointParams   url.Values
	AuthStyle        oauth2.AuthStyle
	AllowInsecureURL bool
	EarlyExpiry      time.Duration
	Now              func() time.Time
}

// ClientCredentialsError reports token endpoint failure without rendering the
// endpoint, client identity, secret, response, or underlying cause.
type ClientCredentialsError struct {
	Cause error
}

// Error implements error without rendering sensitive token endpoint data.
func (*ClientCredentialsError) Error() string {
	return "OAuth2 client credentials request failed"
}

// Unwrap returns the token request failure.
func (err *ClientCredentialsError) Unwrap() error {
	return err.Cause
}

// Error implements error without rendering the source failure or token.
func (*OAuth2TokenError) Error() string {
	return "OAuth2 token acquisition failed"
}

// Unwrap returns the token-source or validation failure.
func (err *OAuth2TokenError) Unwrap() error {
	return err.Cause
}

// NewOAuth2Auth adapts a golang.org/x/oauth2 TokenSource to an immutable
// request editor. The source is wrapped with oauth2.ReuseTokenSource so valid
// tokens are shared and refresh calls are serialized.
func NewOAuth2Auth(source oauth2.TokenSource) (RequestEditor, error) {
	if nilLike(source) {
		return nil, fmt.Errorf("%w: OAuth2 token source is nil", ErrInvalidAuthentication)
	}
	reusable := oauth2.ReuseTokenSource(nil, source)

	return NewContextOAuth2Auth(ContextTokenSourceFunc(func(context.Context) (*oauth2.Token, error) {
		return reusable.Token()
	}))
}

// NewContextOAuth2Auth returns an editor that passes each request context to a
// context-aware token source.
func NewContextOAuth2Auth(source ContextTokenSource) (RequestEditor, error) {
	if nilLike(source) {
		return nil, fmt.Errorf("%w: OAuth2 context token source is nil", ErrInvalidAuthentication)
	}

	return oauth2AuthEditor{source: source}, nil
}

// NewClientCredentialsTokenSource returns a context-aware, concurrency-safe
// OAuth2 client-credentials source. One caller performs a refresh while other
// callers wait cancelably. Token endpoint calls use Client.HTTPClient directly
// so integration middleware cannot recursively authenticate or retry them.
func NewClientCredentialsTokenSource(options ClientCredentialsOptions) (*ClientCredentialsTokenSource, error) {
	configuration, earlyExpiry, now, err := validateClientCredentialsOptions(options)
	if err != nil {
		return nil, err
	}

	return &ClientCredentialsTokenSource{
		client:      options.Client,
		config:      configuration,
		earlyExpiry: earlyExpiry,
		now:         now,
	}, nil
}

// ClientCredentialsTokenSource coordinates cached client-credentials tokens.
type ClientCredentialsTokenSource struct {
	client      *Client
	config      clientcredentials.Config
	earlyExpiry time.Duration
	now         func() time.Time

	mu         sync.Mutex
	token      *oauth2.Token
	refreshing bool
	refreshed  chan struct{}
}

// Token returns an independent token copy or refreshes it using ctx.
func (source *ClientCredentialsTokenSource) Token(ctx context.Context) (*oauth2.Token, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: token context is nil", ErrInvalidAuthentication)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for {
		if source.client.context.Err() != nil {
			return nil, ErrClientClosed
		}
		source.mu.Lock()
		if validClientCredentialsToken(source.token, source.now(), source.earlyExpiry) {
			token := cloneOAuth2Token(source.token)
			source.mu.Unlock()

			return token, nil
		}
		if source.refreshing {
			refreshed := source.refreshed
			source.mu.Unlock()
			select {
			case <-refreshed:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-source.client.context.Done():
				return nil, ErrClientClosed
			}
		}
		source.refreshing = true
		source.refreshed = make(chan struct{})
		source.mu.Unlock()

		token, err := source.fetch(ctx)
		source.mu.Lock()
		if err == nil {
			source.token = cloneOAuth2Token(token)
		}
		source.refreshing = false
		close(source.refreshed)
		source.mu.Unlock()
		if err != nil {
			return nil, err
		}

		return cloneOAuth2Token(token), nil
	}
}

func (source *ClientCredentialsTokenSource) fetch(ctx context.Context) (*oauth2.Token, error) {
	requestContext, cancel := context.WithCancel(ctx)
	stopClientCancellation := context.AfterFunc(source.client.context, cancel)
	defer func() {
		stopClientCancellation()
		cancel()
	}()

	standardClient := *source.client.httpClient
	standardClient.Jar = nil
	requestContext = context.WithValue(requestContext, oauth2.HTTPClient, &standardClient)
	token, err := source.config.Token(requestContext)
	if source.client.context.Err() != nil {
		return nil, &ClientCredentialsError{Cause: ErrClientClosed}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, &ClientCredentialsError{Cause: ctxErr}
	}
	if err != nil {
		return nil, &ClientCredentialsError{Cause: err}
	}
	if !validClientCredentialsToken(token, source.now(), source.earlyExpiry) {
		return nil, &ClientCredentialsError{Cause: ErrInvalidOAuth2Token}
	}
	authorization := token.Type() + " " + token.AccessToken
	if !validHeaderValue(authorization) {
		return nil, &ClientCredentialsError{Cause: ErrInvalidOAuth2Token}
	}

	return token, nil
}

func validateClientCredentialsOptions(
	options ClientCredentialsOptions,
) (clientcredentials.Config, time.Duration, func() time.Time, error) {
	if nilLike(options.Client) || options.ClientID == "" || options.ClientSecret == "" {
		return clientcredentials.Config{}, 0, nil,
			fmt.Errorf("%w: client credentials policy is incomplete", ErrInvalidAuthentication)
	}
	options.Client.mu.Lock()
	closed := options.Client.closed
	options.Client.mu.Unlock()
	if closed {
		return clientcredentials.Config{}, 0, nil,
			fmt.Errorf("%w: client is closed", ErrInvalidAuthentication)
	}
	tokenURL, err := url.Parse(options.TokenURL)
	if err != nil || tokenURL.Host == "" || tokenURL.User != nil || tokenURL.RawQuery != "" || tokenURL.Fragment != "" ||
		(tokenURL.Scheme != "https" && (!options.AllowInsecureURL || tokenURL.Scheme != "http")) {
		return clientcredentials.Config{}, 0, nil,
			fmt.Errorf("%w: token URL is unsafe", ErrInvalidAuthentication)
	}
	if options.EarlyExpiry < 0 {
		return clientcredentials.Config{}, 0, nil,
			fmt.Errorf("%w: early expiry is negative", ErrInvalidAuthentication)
	}
	earlyExpiry := options.EarlyExpiry
	if earlyExpiry == 0 {
		earlyExpiry = defaultOAuth2EarlyExpiry
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	authStyle := options.AuthStyle
	if authStyle == oauth2.AuthStyleAutoDetect {
		authStyle = oauth2.AuthStyleInHeader
	}
	if authStyle != oauth2.AuthStyleInHeader && authStyle != oauth2.AuthStyleInParams {
		return clientcredentials.Config{}, 0, nil,
			fmt.Errorf("%w: client authentication style is unknown", ErrInvalidAuthentication)
	}
	scopes := append([]string(nil), options.Scopes...)
	for _, scope := range scopes {
		if !validOAuth2Scope(scope) {
			return clientcredentials.Config{}, 0, nil,
				fmt.Errorf("%w: OAuth2 scope is malformed", ErrInvalidAuthentication)
		}
	}
	parameters, err := cloneOAuth2EndpointParams(options.EndpointParams)
	if err != nil {
		return clientcredentials.Config{}, 0, nil, err
	}

	return clientcredentials.Config{
		ClientID:       options.ClientID,
		ClientSecret:   options.ClientSecret,
		TokenURL:       tokenURL.String(),
		Scopes:         scopes,
		EndpointParams: parameters,
		AuthStyle:      authStyle,
	}, earlyExpiry, now, nil
}

func cloneOAuth2EndpointParams(parameters url.Values) (url.Values, error) {
	clone := make(url.Values, len(parameters))
	for name, values := range parameters {
		if name == "client_id" || name == "client_secret" || name == "grant_type" || name == "scope" || name == "" {
			return nil, fmt.Errorf("%w: reserved token endpoint parameter", ErrInvalidAuthentication)
		}
		clone[name] = append([]string(nil), values...)
	}

	return clone, nil
}

func validOAuth2Scope(scope string) bool {
	if scope == "" {
		return false
	}
	for _, character := range scope {
		if character != 0x21 && (character < 0x23 || character > 0x5b) && (character < 0x5d || character > 0x7e) {
			return false
		}
	}

	return true
}

func validClientCredentialsToken(token *oauth2.Token, now time.Time, earlyExpiry time.Duration) bool {
	if token == nil || token.AccessToken == "" {
		return false
	}
	if token.Expiry.IsZero() {
		return true
	}

	return token.Expiry.After(now.Add(earlyExpiry))
}

func cloneOAuth2Token(token *oauth2.Token) *oauth2.Token {
	if token == nil {
		return nil
	}
	clone := *token

	return &clone
}

type oauth2AuthEditor struct {
	source ContextTokenSource
}

func (editor oauth2AuthEditor) EditRequest(request *http.Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidAuthentication)
	}
	if err := request.Context().Err(); err != nil {
		return err
	}
	token, err := editor.source.Token(request.Context())
	if err != nil {
		return &OAuth2TokenError{Cause: err}
	}
	if err := request.Context().Err(); err != nil {
		return err
	}
	if token == nil || !token.Valid() {
		return &OAuth2TokenError{Cause: ErrInvalidOAuth2Token}
	}
	authorization := token.Type() + " " + token.AccessToken
	if !validHeaderValue(authorization) {
		return &OAuth2TokenError{Cause: ErrInvalidOAuth2Token}
	}
	request.Header.Set("Authorization", authorization)

	return nil
}
