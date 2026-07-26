// Package basic provides Basic credential authenticators.
package basic

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
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
// fixed-length keyed secret digests are retained and every entry is compared.
type Static struct {
	digestKey []byte
	entries   []staticEntry
}

// NewStatic validates and copies entries.
func NewStatic(entries []Entry) (*Static, error) {
	if len(entries) == 0 || len(entries) > MaxEntries {
		return nil, fmt.Errorf("%w: Basic entry count", authentication.ErrInvalidConfiguration)
	}

	digestKey := []byte(rand.Text())
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

		username := secretDigest(digestKey, "username", entry.Username)
		password := secretDigest(digestKey, "password", entry.Password)
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

	return &Static{digestKey: digestKey, entries: built}, nil
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

	username := secretDigest(s.digestKey, "username", basicCredential.Username())
	password := secretDigest(s.digestKey, "password", basicCredential.Password())
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

func secretDigest(key []byte, domain, value string) [sha256.Size]byte {
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(value))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

var _ authentication.Authenticator = (*Static)(nil)
