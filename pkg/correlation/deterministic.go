package correlation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalidDerivation reports unsafe deterministic derivation input or
// configuration.
var ErrInvalidDerivation = errors.New("correlation: invalid deterministic derivation")

// DeterministicOptions configure an explicitly opted-in stable strategy.
type DeterministicOptions struct {
	Domain  string
	Version uint32
	Key     []byte
	Length  int
}

// Deterministic derives linkable IDs for an explicitly stable workflow. It is
// never selected by Factory defaults and should be keyed for private inputs.
type Deterministic struct {
	domain  string
	version uint32
	key     []byte
	length  int
}

// NewDeterministic validates and copies deterministic strategy configuration.
func NewDeterministic(options DeterministicOptions) (*Deterministic, error) {
	prefix := fmt.Sprintf("d%d_", options.Version)
	maximum := len(prefix) + base64.RawURLEncoding.EncodedLen(sha256.Size)
	if options.Domain == "" || len(options.Domain) > 128 || options.Version == 0 ||
		options.Length < len(prefix)+16 || options.Length > maximum || len(options.Key) > 1024 {
		return nil, fmt.Errorf("%w: configuration", ErrInvalidDerivation)
	}
	if err := validate(options.Domain, Policy{MaxLength: 128}); err != nil {
		return nil, fmt.Errorf("%w: domain", ErrInvalidDerivation)
	}
	return &Deterministic{
		domain: options.Domain, version: options.Version,
		key: append([]byte(nil), options.Key...), length: options.Length,
	}, nil
}

// Derive hashes input with length-delimited, versioned domain separation.
func (strategy *Deterministic) Derive(input []byte) (CorrelationID, error) {
	if strategy == nil || len(input) == 0 || len(input) > 1<<20 {
		return "", fmt.Errorf("%w: input", ErrInvalidDerivation)
	}
	hash := hmac.New(sha256.New, strategy.key)
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], strategy.version)
	_, _ = hash.Write([]byte("go-correlation/deterministic\x00"))
	_, _ = hash.Write(encoded[:])
	// #nosec G115 -- domain length is bounded to 128 bytes at construction.
	binary.BigEndian.PutUint32(encoded[:], uint32(len(strategy.domain)))
	_, _ = hash.Write(encoded[:])
	_, _ = hash.Write([]byte(strategy.domain))
	// #nosec G115 -- input length is bounded to one mebibyte above.
	binary.BigEndian.PutUint32(encoded[:], uint32(len(input)))
	_, _ = hash.Write(encoded[:])
	_, _ = hash.Write(input)

	prefix := fmt.Sprintf("d%d_", strategy.version)
	text := prefix + base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
	return CorrelationID(text[:strategy.length]), nil
}
