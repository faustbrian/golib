// Package basic provides Basic credential authenticators.
package basic

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

// MaxEntries bounds the work performed for one static Basic authentication.
const MaxEntries = 256

// Entry configures one accepted username and password pair.
type Entry struct {
	Username  string
	Password  string
	Principal authentication.PrincipalSpec
}

type staticEntry struct {
	username [sha256.Size]byte
	password [sha256.Size]byte
	result   authentication.Result
}

// Static validates Basic credentials against a bounded immutable set. Only
// fixed-length secret digests are retained and every entry is compared.
type Static struct{ entries []staticEntry }

// NewStatic validates and copies entries.
func NewStatic(entries []Entry) (*Static, error) {
	if len(entries) == 0 || len(entries) > MaxEntries {
		return nil, fmt.Errorf("%w: Basic entry count", authentication.ErrInvalidConfiguration)
	}

	built := make([]staticEntry, 0, len(entries))
	seen := make(map[credentialDigest]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Username == "" || entry.Password == "" ||
			containsControl(entry.Username) || containsControl(entry.Password) {
			return nil, fmt.Errorf("%w: invalid Basic credential", authentication.ErrInvalidConfiguration)
		}
		if entry.Principal.Method != "" && entry.Principal.Method != "basic" {
			return nil, fmt.Errorf("%w: Basic principal method", authentication.ErrInvalidConfiguration)
		}

		username := sha256.Sum256([]byte(entry.Username))
		password := sha256.Sum256([]byte(entry.Password))
		digest := credentialDigest{username: username, password: password}
		if _, exists := seen[digest]; exists {
			return nil, fmt.Errorf("%w: duplicate Basic credential", authentication.ErrInvalidConfiguration)
		}
		seen[digest] = struct{}{}

		spec := entry.Principal
		spec.Method = "basic"
		principal, err := authentication.NewPrincipal(spec)
		if err != nil {
			return nil, fmt.Errorf("%w: Basic principal", authentication.ErrInvalidConfiguration)
		}
		// NewPrincipal guarantees this is a concrete identity.
		result, _ := authentication.NewAuthenticatedResult(principal)
		built = append(built, staticEntry{username: username, password: password, result: result})
	}

	return &Static{entries: built}, nil
}

func containsControl(value string) bool {
	for _, character := range []byte(value) {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

// Authenticate validates one Basic credential in constant work relative to
// the configured entry count.
func (s *Static) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	basicCredential, ok := credential.(authentication.BasicCredential)
	if !ok || basicCredential.Username() == "" || basicCredential.Password() == "" {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}

	username := sha256.Sum256([]byte(basicCredential.Username()))
	password := sha256.Sum256([]byte(basicCredential.Password()))
	matched := 0
	var result authentication.Result
	for _, entry := range s.entries {
		current := subtle.ConstantTimeCompare(username[:], entry.username[:]) &
			subtle.ConstantTimeCompare(password[:], entry.password[:])
		if current == 1 && matched == 0 {
			result = entry.result
		}
		matched |= current
	}
	if matched != 1 {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureRejected)
	}

	return result, nil
}

type credentialDigest struct {
	username [sha256.Size]byte
	password [sha256.Size]byte
}

var _ authentication.Authenticator = (*Static)(nil)
