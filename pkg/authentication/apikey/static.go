// Package apikey provides static and callback API-key authenticators.
package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"sync"
	"sync/atomic"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

// MaxEntries bounds active key candidates and per-request comparison work.
const MaxEntries = 256

// Entry configures one active key with a deterministic non-secret identifier.
type Entry struct {
	ID        string
	Key       string
	Principal authentication.PrincipalSpec
}

type staticEntry struct {
	id     [sha256.Size]byte
	key    [sha256.Size]byte
	result authentication.Result
}

type keySet struct{ entries []staticEntry }

// Static validates API keys against an atomically replaceable bounded key set.
type Static struct {
	digestKey [sha256.Size]byte
	keyOnce   sync.Once
	set       atomic.Pointer[keySet]
}

// NewStatic validates and copies the initial active key set.
func NewStatic(entries []Entry) (*Static, error) {
	authenticator := &Static{}
	if err := authenticator.Replace(entries); err != nil {
		return nil, err
	}
	return authenticator, nil
}

// Replace atomically replaces all active keys after validating the complete
// candidate set. A failed replacement leaves the previous set active.
func (s *Static) Replace(entries []Entry) error {
	digestKey := s.key()
	built, err := buildKeySet(entries, digestKey)
	if err != nil {
		return err
	}
	s.set.Store(built)
	return nil
}

// Authenticate validates one API key against a single immutable key-set snapshot.
func (s *Static) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	apiKey, ok := credential.(authentication.APIKeyCredential)
	if !ok || apiKey.KeyID() == "" || apiKey.Key() == "" {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}

	digestKey := s.key()
	id := secretDigest(digestKey, "id", apiKey.KeyID())
	key := secretDigest(digestKey, "key", apiKey.Key())
	matched := 0
	var result authentication.Result
	set := s.set.Load()
	if set == nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}
	for _, entry := range set.entries {
		current := subtle.ConstantTimeCompare(id[:], entry.id[:]) &
			subtle.ConstantTimeCompare(key[:], entry.key[:])
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

func buildKeySet(entries []Entry, digestKey []byte) (*keySet, error) {
	if len(entries) == 0 || len(entries) > MaxEntries {
		return nil, fmt.Errorf("%w: API-key entry count", authentication.ErrInvalidConfiguration)
	}

	built := make([]staticEntry, 0, len(entries))
	ids := make(map[[sha256.Size]byte]struct{}, len(entries))
	keys := make(map[[sha256.Size]byte]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Key == "" {
			return nil, fmt.Errorf("%w: empty API-key data", authentication.ErrInvalidConfiguration)
		}
		if entry.Principal.Method != "" && entry.Principal.Method != "api_key" {
			return nil, fmt.Errorf("%w: API-key principal method", authentication.ErrInvalidConfiguration)
		}
		id := secretDigest(digestKey, "id", entry.ID)
		key := secretDigest(digestKey, "key", entry.Key)
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("%w: duplicate API-key ID", authentication.ErrInvalidConfiguration)
		}
		if _, exists := keys[key]; exists {
			return nil, fmt.Errorf("%w: duplicate API key", authentication.ErrInvalidConfiguration)
		}
		ids[id] = struct{}{}
		keys[key] = struct{}{}

		spec := entry.Principal
		spec.Method = "api_key"
		principal, err := authentication.NewPrincipal(spec)
		if err != nil {
			return nil, fmt.Errorf("%w: API-key principal", authentication.ErrInvalidConfiguration)
		}
		// NewPrincipal guarantees this is a concrete identity.
		result, _ := authentication.NewAuthenticatedResult(principal)
		built = append(built, staticEntry{id: id, key: key, result: result})
	}

	return &keySet{entries: built}, nil
}

func (s *Static) key() []byte {
	s.keyOnce.Do(func() {
		copy(s.digestKey[:], rand.Text())
	})
	return s.digestKey[:]
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
