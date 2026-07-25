package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

var (
	// ErrInvalidSession indicates invalid cookie or persistence policy.
	ErrInvalidSession = errors.New("invalid HTTP session")
	// ErrSessionDisabled indicates that a client has no session configuration.
	ErrSessionDisabled = errors.New("HTTP session is disabled")
	// ErrSessionPersistenceUnavailable indicates that no persistence port exists.
	ErrSessionPersistenceUnavailable = errors.New("HTTP session persistence is unavailable")
)

const (
	defaultSessionPersistenceTimeout = 5 * time.Second
	sessionMiddlewarePriority        = -1000
)

// CookieJarOwnership controls whether Client.Close closes a custom jar that
// also implements io.Closer. Internally created jars are always owned.
type CookieJarOwnership uint8

const (
	// CookieJarBorrowed leaves a custom jar under caller ownership.
	CookieJarBorrowed CookieJarOwnership = iota
	// CookieJarOwned transfers a closable custom jar to Client.
	CookieJarOwned
)

// CookieRedirectPolicy controls whether jar-selected cookies may cross the
// initial logical operation origin during redirects.
type CookieRedirectPolicy uint8

const (
	// CookieRedirectSameOrigin strips cookies when a redirect changes origin.
	CookieRedirectSameOrigin CookieRedirectPolicy = iota
	// CookieRedirectJar trusts the jar's domain, path, security, and suffix rules.
	CookieRedirectJar
)

// SessionPersistence stores and restores one configured cookie jar. Methods
// must honor context cancellation and be safe for concurrent callers.
type SessionPersistence interface {
	Load(context.Context, http.CookieJar) error
	Save(context.Context, http.CookieJar) error
}

// SessionPersistenceOperation identifies a cookie persistence action.
type SessionPersistenceOperation uint8

const (
	// SessionPersistenceLoad restores cookies into a jar.
	SessionPersistenceLoad SessionPersistenceOperation = iota
	// SessionPersistenceSave stores cookies from a jar.
	SessionPersistenceSave
)

// String returns a stable operation name.
func (operation SessionPersistenceOperation) String() string {
	switch operation {
	case SessionPersistenceLoad:
		return "load"
	case SessionPersistenceSave:
		return "save"
	default:
		return fmt.Sprintf("operation(%d)", operation)
	}
}

// SessionConfig opts a client into isolated cookie and persistence behavior.
type SessionConfig struct {
	Jar                http.CookieJar
	JarOwnership       CookieJarOwnership
	PublicSuffixList   cookiejar.PublicSuffixList
	RedirectPolicy     CookieRedirectPolicy
	Persistence        SessionPersistence
	LoadOnStart        bool
	SaveOnClose        bool
	PersistenceTimeout time.Duration
}

// SessionPersistenceError reports a load or save failure without rendering
// its cause, which may contain cookie values or storage identifiers.
type SessionPersistenceError struct {
	Operation SessionPersistenceOperation
	Cause     error
}

// SessionCloseError reports an owned jar close failure without rendering it.
type SessionCloseError struct {
	Cause error
}

// Error implements error without rendering jar state or cookie data.
func (*SessionCloseError) Error() string {
	return "HTTP session close failed"
}

// Unwrap returns the jar close failure.
func (err *SessionCloseError) Unwrap() error {
	return err.Cause
}

// Error implements error without rendering persistence data.
func (err *SessionPersistenceError) Error() string {
	return fmt.Sprintf("HTTP session %s failed", err.Operation)
}

// Unwrap returns the persistence failure.
func (err *SessionPersistenceError) Unwrap() error {
	return err.Cause
}

type sessionOriginContextKey struct{}

type clientSession struct {
	jar          http.CookieJar
	ownedJar     bool
	persistence  SessionPersistence
	timeout      time.Duration
	saveOnClose  bool
	persistenceM sync.Mutex
}

func newClientSession(config *SessionConfig) (*clientSession, []Middleware, error) {
	if config == nil {
		return nil, nil, nil
	}
	if config.JarOwnership > CookieJarOwned {
		return nil, nil, fmt.Errorf("%w: unknown jar ownership", ErrInvalidSession)
	}
	if config.RedirectPolicy > CookieRedirectJar {
		return nil, nil, fmt.Errorf("%w: unknown redirect policy", ErrInvalidSession)
	}
	if config.PersistenceTimeout < 0 {
		return nil, nil, fmt.Errorf("%w: persistence timeout is negative", ErrInvalidSession)
	}
	if (config.LoadOnStart || config.SaveOnClose) && nilLike(config.Persistence) {
		return nil, nil, fmt.Errorf("%w: persistence lifecycle has no port", ErrInvalidSession)
	}
	if config.Jar != nil && nilLike(config.Jar) {
		return nil, nil, fmt.Errorf("%w: cookie jar is nil", ErrInvalidSession)
	}
	if config.Jar != nil && config.PublicSuffixList != nil {
		return nil, nil, fmt.Errorf("%w: public suffix policy cannot replace a custom jar policy", ErrInvalidSession)
	}

	jar := config.Jar
	owned := config.JarOwnership == CookieJarOwned
	if jar == nil {
		publicSuffixList := config.PublicSuffixList
		if publicSuffixList == nil {
			publicSuffixList = publicsuffix.List
		}
		created, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicSuffixList})
		jar = created
		owned = true
	}
	timeout := config.PersistenceTimeout
	if timeout == 0 {
		timeout = defaultSessionPersistenceTimeout
	}
	session := &clientSession{
		jar:         jar,
		ownedJar:    owned,
		persistence: config.Persistence,
		timeout:     timeout,
		saveOnClose: config.SaveOnClose,
	}
	middleware := newSessionMiddleware(config.RedirectPolicy)

	return session, middleware, nil
}

func newSessionMiddleware(policy CookieRedirectPolicy) []Middleware {
	operation := Middleware{
		information: MiddlewareInfo{
			Name:     "httpclient.session",
			Scope:    ScopeOperation,
			Layer:    MiddlewareClient,
			Stage:    StageRequest,
			Priority: sessionMiddlewarePriority,
		},
		around: func(request *http.Request, next Next) (*http.Response, error) {
			origin, originErr := sessionRequestOrigin(request)
			if originErr != nil {
				return nil, originErr
			}

			return next(request.WithContext(context.WithValue(request.Context(), sessionOriginContextKey{}, origin)))
		},
	}
	attempt := operation
	attempt.information.Scope = ScopeAttempt
	attempt.around = func(request *http.Request, next Next) (*http.Response, error) {
		initialOrigin, ok := request.Context().Value(sessionOriginContextKey{}).(string)
		if !ok {
			return nil, fmt.Errorf("%w: session operation context is missing", ErrInvalidSession)
		}
		if policy == CookieRedirectSameOrigin {
			origin, originErr := sessionRequestOrigin(request)
			if originErr != nil {
				return nil, originErr
			}
			if origin != initialOrigin {
				request.Header.Del("Cookie")
			}
		}

		return next(request)
	}

	return []Middleware{operation, attempt}
}

func sessionRequestOrigin(request *http.Request) (string, error) {
	if request == nil || request.URL == nil {
		return "", fmt.Errorf("%w: request URL is nil", ErrInvalidSession)
	}
	origin, err := canonicalOrigin(request.URL)
	if err != nil {
		return "", fmt.Errorf("%w: request origin is invalid", ErrInvalidSession)
	}

	return origin, nil
}

func (session *clientSession) load(ctx context.Context) error {
	if nilLike(session.persistence) {
		return ErrSessionPersistenceUnavailable
	}
	session.persistenceM.Lock()
	defer session.persistenceM.Unlock()
	if err := session.persistence.Load(ctx, session.jar); err != nil {
		return &SessionPersistenceError{Operation: SessionPersistenceLoad, Cause: err}
	}

	return nil
}

func (session *clientSession) save(ctx context.Context) error {
	if nilLike(session.persistence) {
		return ErrSessionPersistenceUnavailable
	}
	session.persistenceM.Lock()
	defer session.persistenceM.Unlock()
	if err := session.persistence.Save(ctx, session.jar); err != nil {
		return &SessionPersistenceError{Operation: SessionPersistenceSave, Cause: err}
	}

	return nil
}

func (session *clientSession) closeJar() error {
	if !session.ownedJar {
		return nil
	}
	if closer, ok := session.jar.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			return &SessionCloseError{Cause: err}
		}
	}

	return nil
}
