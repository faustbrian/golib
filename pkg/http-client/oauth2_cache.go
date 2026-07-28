package httpclient

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// TokenCacheOptions configures a context-aware cache around a caller-owned
// OAuth2 token source. Client owns cancellation and lifecycle.
type TokenCacheOptions struct {
	Client      *Client
	Source      ContextTokenSource
	EarlyExpiry time.Duration
	Now         func() time.Time
}

// TokenCacheError reports an upstream refresh failure without rendering its
// cause, which may contain credentials or provider response data.
type TokenCacheError struct {
	Cause error
}

// Error implements error without rendering token or upstream failure data.
func (*TokenCacheError) Error() string {
	return "OAuth2 token cache refresh failed"
}

// Unwrap returns the upstream, cancellation, lifecycle, or validation cause.
func (err *TokenCacheError) Unwrap() error {
	return err.Cause
}

// CachedTokenSource coordinates one caller-owned token refresh while other
// callers wait cancelably. Every successful caller receives an independent
// token copy. Invalidate removes only the exact token observed by a rejected
// provider request, so a concurrent newer token cannot be discarded.
type CachedTokenSource struct {
	client      *Client
	source      ContextTokenSource
	earlyExpiry time.Duration
	now         func() time.Time

	mu         sync.Mutex
	token      *oauth2.Token
	refreshing bool
	refreshed  chan struct{}
}

func (source *CachedTokenSource) validToken(token *oauth2.Token) bool {
	return validClientCredentialsToken(
		token,
		source.now(),
		source.earlyExpiry,
	)
}

// NewCachedTokenSource wraps a context-aware caller-owned token source with a
// client-bounded, concurrency-safe cache and explicit invalidation.
func NewCachedTokenSource(options TokenCacheOptions) (*CachedTokenSource, error) {
	if nilLike(options.Client) || nilLike(options.Source) {
		return nil, fmt.Errorf("%w: token cache policy is incomplete", ErrInvalidAuthentication)
	}
	options.Client.mu.Lock()
	closed := options.Client.closed
	options.Client.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("%w: client is closed", ErrInvalidAuthentication)
	}
	if options.EarlyExpiry < 0 {
		return nil, fmt.Errorf("%w: early expiry is negative", ErrInvalidAuthentication)
	}
	earlyExpiry := options.EarlyExpiry
	if earlyExpiry == 0 {
		earlyExpiry = defaultOAuth2EarlyExpiry
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &CachedTokenSource{
		client: options.Client, source: options.Source,
		earlyExpiry: earlyExpiry, now: now,
	}, nil
}

// Token returns a cached independent token or coordinates one bounded refresh.
func (source *CachedTokenSource) Token(ctx context.Context) (*oauth2.Token, error) {
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

// Invalidate removes the cached token only when accessToken is the exact token
// currently cached. Empty, stale, and already-invalidated values return false.
func (source *CachedTokenSource) Invalidate(accessToken string) bool {
	return invalidateToken(&source.mu, &source.token, accessToken)
}

func (source *CachedTokenSource) fetch(ctx context.Context) (*oauth2.Token, error) {
	requestContext, cancel := context.WithCancel(ctx)
	stopClientCancellation := context.AfterFunc(source.client.context, cancel)
	defer func() {
		stopClientCancellation()
		cancel()
	}()

	token, err := source.source.Token(requestContext)
	if source.client.context.Err() != nil {
		return nil, &TokenCacheError{Cause: ErrClientClosed}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, &TokenCacheError{Cause: ctxErr}
	}
	if err != nil {
		return nil, &TokenCacheError{Cause: err}
	}
	if !validClientCredentialsToken(token, source.now(), source.earlyExpiry) {
		return nil, &TokenCacheError{Cause: ErrInvalidOAuth2Token}
	}
	authorization := token.Type() + " " + token.AccessToken
	if !validHeaderValue(authorization) {
		return nil, &TokenCacheError{Cause: ErrInvalidOAuth2Token}
	}

	return token, nil
}

func invalidateToken(mu *sync.Mutex, token **oauth2.Token, accessToken string) bool {
	if accessToken == "" {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if *token == nil || subtle.ConstantTimeCompare(
		[]byte((*token).AccessToken),
		[]byte(accessToken),
	) != 1 {
		return false
	}
	*token = nil

	return true
}

var _ error = (*TokenCacheError)(nil)
var _ ContextTokenSource = (*CachedTokenSource)(nil)
