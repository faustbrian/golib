package passwordauth

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	password "github.com/faustbrian/golib/pkg/password"
)

var (
	// ErrInvalidConfig reports a missing service, lookup, or valid dummy hash.
	ErrInvalidConfig = errors.New("passwordauth: invalid configuration")
	// ErrRejected reports a missing identity or password mismatch.
	ErrRejected = errors.New("passwordauth: authentication rejected")
	// ErrUnavailable reports lookup, stored-data, resource, or entropy failure.
	ErrUnavailable = errors.New("passwordauth: authentication unavailable")
	// ErrCanceled reports caller cancellation or deadline expiration.
	ErrCanceled = errors.New("passwordauth: authentication canceled")
)

// Error is a classified authentication adapter error that omits Cause text.
type Error struct {
	kind  error
	cause error
}

func newError(kind, cause error) *Error { return &Error{kind: kind, cause: cause} }

// Kind returns the stable passwordauth sentinel.
func (e *Error) Kind() error { return e.kind }

// Cause returns the underlying operational error without formatting it.
func (e *Error) Cause() error { return e.cause }

// Error returns only the stable classification.
func (e *Error) Error() string { return e.kind.Error() }

// Unwrap exposes classification and cause to errors.Is/errors.As.
func (e *Error) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

// Record is an application-owned subject and encoded password hash. Diagnostic
// formatting is redacted; EncodedHash is accessed explicitly for verification.
type Record struct {
	// Subject is the stable application identity.
	Subject string
	// EncodedHash is the current database value.
	EncodedHash string
}

// String returns a redacted diagnostic representation.
func (Record) String() string { return "password record [redacted]" }

// GoString returns a redacted Go-syntax representation.
func (Record) GoString() string { return "passwordauth.Record{redacted}" }

// Format redacts every fmt formatting verb.
func (Record) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, "password record [redacted]")
}

// Lookup retrieves an application-owned record without repository ownership.
type Lookup interface {
	// LookupPassword returns a record, its presence, and an operational error.
	LookupPassword(context.Context, string) (Record, bool, error)
}

// Config contains all mandatory authentication adapter collaborators.
type Config struct {
	// Passwords performs bounded verification and upgrades.
	Passwords *password.Service
	// Lookup retrieves application-owned records.
	Lookup Lookup
	// DummyHash is valid synthetic work used when the username is absent.
	DummyHash string
}

// Authenticator verifies lookup records and returns explicit CAS upgrade data.
type Authenticator struct {
	passwords *password.Service
	lookup    Lookup
	dummy     password.EncodedHash
}

// New validates all collaborators and parses DummyHash before accepting work.
func New(config Config) (*Authenticator, error) {
	if config.Passwords == nil || nilLookup(config.Lookup) || config.DummyHash == "" {
		return nil, newError(ErrInvalidConfig, nil)
	}
	dummy, err := password.ParseEncodedHash(config.DummyHash, config.Passwords.Policy().Limits())
	if err != nil {
		return nil, newError(ErrInvalidConfig, err)
	}
	return &Authenticator{passwords: config.Passwords, lookup: config.Lookup, dummy: dummy}, nil
}

func nilLookup(lookup Lookup) bool {
	if lookup == nil {
		return true
	}
	reflected := reflect.ValueOf(lookup)
	kind := reflected.Kind()
	return (kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface || kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice) && reflected.IsNil()
}

// Upgrade is an immutable optimistic compare-and-swap pair.
type Upgrade struct {
	expected    password.EncodedHash
	replacement password.EncodedHash
}

// Required reports whether a durable conditional update is needed.
func (u Upgrade) Required() bool { return u.replacement.String() != "" }

// Expected returns the hash that must still be stored for CAS to succeed.
func (u Upgrade) Expected() password.EncodedHash { return u.expected }

// Replacement returns the newly computed hash for the conditional update.
func (u Upgrade) Replacement() password.EncodedHash { return u.replacement }

// String returns a redacted diagnostic representation.
func (Upgrade) String() string { return "password upgrade [redacted]" }

// GoString returns a redacted Go-syntax representation.
func (Upgrade) GoString() string { return "passwordauth.Upgrade{redacted}" }

// Result is a successful stable subject plus optional CAS upgrade.
type Result struct {
	subject string
	upgrade Upgrade
}

// Subject returns the stable application identity.
func (r Result) Subject() string { return r.subject }

// Upgrade returns the optional immutable CAS pair.
func (r Result) Upgrade() Upgrade { return r.upgrade }

// String returns a secret-safe diagnostic representation.
func (Result) String() string { return "password authentication result" }

// GoString returns a redacted Go-syntax representation.
func (Result) GoString() string { return "passwordauth.Result{redacted}" }

// Authenticate performs lookup, dummy work for absence, verification, and an
// in-memory replacement hash. It never writes persistence or constructs users.
func (a *Authenticator) Authenticate(ctx context.Context, username string, secret []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, newError(ErrCanceled, err)
	}
	record, found, err := a.lookup.LookupPassword(ctx, username)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, newError(ErrCanceled, err)
		}
		return Result{}, newError(ErrUnavailable, err)
	}
	encoded := a.dummy.String()
	expected := password.EncodedHash{}
	if found {
		if record.Subject == "" || record.EncodedHash == "" {
			return Result{}, newError(ErrUnavailable, nil)
		}
		expected, err = password.ParseEncodedHash(record.EncodedHash, a.passwords.Policy().Limits())
		if err != nil {
			return Result{}, newError(ErrUnavailable, err)
		}
		encoded = expected.String()
	}
	verification, replacement, err := a.passwords.VerifyAndUpgrade(ctx, secret, encoded)
	if err != nil {
		switch {
		case errors.Is(err, password.ErrCanceled):
			return Result{}, newError(ErrCanceled, err)
		case errors.Is(err, password.ErrMismatch):
			return Result{}, newError(ErrRejected, err)
		default:
			return Result{}, newError(ErrUnavailable, err)
		}
	}
	if !found {
		return Result{}, newError(ErrRejected, nil)
	}
	upgrade := Upgrade{}
	if verification.NeedsRehash() {
		upgrade = Upgrade{expected: expected, replacement: replacement}
	}
	return Result{subject: record.Subject, upgrade: upgrade}, nil
}
