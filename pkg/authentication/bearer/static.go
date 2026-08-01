package bearer

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

// MaxEntries bounds active static bearer candidates and per-request work.
const MaxEntries = 256

// Entry configures one active static bearer token and its principal.
type Entry struct {
	Token     string
	Principal authentication.PrincipalSpec
}

type staticEntry struct {
	token  [sha256.Size]byte
	result authentication.Result
}

type tokenSet struct{ entries []staticEntry }

// Static validates bearer tokens against an atomically replaceable bounded
// token set. Every well-formed credential is compared with every active entry.
// A Static must not be copied after first use.
type Static struct {
	digestKey [sha256.Size]byte
	keyOnce   sync.Once
	set       atomic.Pointer[tokenSet]
}

// NewStatic validates and copies the initial active token set.
func NewStatic(entries []Entry) (*Static, error) {
	authenticator := &Static{}
	if err := authenticator.Replace(entries); err != nil {
		return nil, err
	}
	return authenticator, nil
}

// Replace atomically replaces all active tokens after validating the complete
// candidate set. A failed replacement leaves the previous set active.
func (s *Static) Replace(entries []Entry) error {
	built, err := buildTokenSet(entries, s.key())
	if err != nil {
		return err
	}
	s.set.Store(built)
	return nil
}

// Authenticate validates one bounded bearer credential against a single
// immutable token-set snapshot.
func (s *Static) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	bearerCredential, ok := credential.(authentication.BearerCredential)
	if !ok || bearerCredential.Token() == "" || len(bearerCredential.Token()) > defaultMaxTokenBytes {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	set := s.set.Load()
	if set == nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}

	token := tokenDigest(s.key(), bearerCredential.Token())
	matched := 0
	var result authentication.Result
	for _, entry := range set.entries {
		current := subtle.ConstantTimeCompare(token[:], entry.token[:])
		if current == 1 {
			result = entry.result
		}
		matched |= current
	}
	if matched != 1 {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureRejected)
	}
	return result, nil
}

func buildTokenSet(entries []Entry, digestKey []byte) (*tokenSet, error) {
	if len(entries) == 0 || len(entries) > MaxEntries {
		return nil, fmt.Errorf("%w: bearer entry count", authentication.ErrInvalidConfiguration)
	}
	built := make([]staticEntry, 0, len(entries))
	tokens := make(map[[sha256.Size]byte]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Token == "" || len(entry.Token) > defaultMaxTokenBytes {
			return nil, fmt.Errorf("%w: bearer token", authentication.ErrInvalidConfiguration)
		}
		if entry.Principal.Method != "" && entry.Principal.Method != "bearer" {
			return nil, fmt.Errorf("%w: bearer principal method", authentication.ErrInvalidConfiguration)
		}
		token := tokenDigest(digestKey, entry.Token)
		if _, exists := tokens[token]; exists {
			return nil, fmt.Errorf("%w: duplicate bearer token", authentication.ErrInvalidConfiguration)
		}
		tokens[token] = struct{}{}
		spec := entry.Principal
		spec.Method = "bearer"
		principal, err := authentication.NewPrincipal(spec)
		if err != nil {
			return nil, fmt.Errorf("%w: bearer principal", authentication.ErrInvalidConfiguration)
		}
		result, _ := authentication.NewAuthenticatedResult(principal)
		built = append(built, staticEntry{token: token, result: result})
	}
	return &tokenSet{entries: built}, nil
}

func (s *Static) key() []byte {
	s.keyOnce.Do(func() {
		copy(s.digestKey[:], rand.Text())
	})
	return s.digestKey[:]
}

func tokenDigest(key []byte, token string) [sha256.Size]byte {
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte("bearer"))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(token))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

var _ authentication.Authenticator = (*Static)(nil)
