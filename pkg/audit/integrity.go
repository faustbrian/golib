package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"time"
)

// IntegrityAlgorithm identifies a standard-library digest construction.
type IntegrityAlgorithm uint8

const (
	// IntegritySHA256 selects an unkeyed SHA-256 chain for corruption detection.
	IntegritySHA256 IntegrityAlgorithm = iota + 1
	// IntegrityHMACSHA256 selects HMAC-SHA-256 with caller-provided keys.
	IntegrityHMACSHA256
)

var ErrKeyUnavailable = errors.New("audit: integrity key is unavailable")

// IntegrityKey is external key-rotation metadata. Bytes are copied and never
// included in records, errors, or diagnostics.
type IntegrityKey struct {
	ID    string
	Bytes []byte
}

// KeyRequest selects the current key for sealing when KeyID is empty, or the
// exact persisted historical key for verification when KeyID is present.
type KeyRequest struct {
	Partition  string
	KeyID      string
	RecordedAt time.Time
}

// KeyProvider selects an HMAC key for a bounded request.
type KeyProvider interface {
	Key(context.Context, KeyRequest) (IntegrityKey, error)
}

// KeyProviderFunc adapts a function to KeyProvider.
type KeyProviderFunc func(context.Context, KeyRequest) (IntegrityKey, error)

// Key invokes the adapted key lookup. Implementations must not expose key
// bytes through errors or diagnostics.
func (provider KeyProviderFunc) Key(ctx context.Context, request KeyRequest) (IntegrityKey, error) {
	if provider == nil || ctx == nil {
		return IntegrityKey{}, invalid("key_provider", "must be assigned")
	}
	return provider(ctx, request)
}

// ChainConfig selects the digest algorithm, external HMAC key source, and
// optional identifier-free observer.
type ChainConfig struct {
	Algorithm IntegrityAlgorithm
	Keys      KeyProvider
	Observer  Observer
}

// ChainLink supplies caller-persisted ordering state for the next record.
// PreviousDigest remains caller-owned and is copied by Seal.
type ChainLink struct {
	Partition      string
	Sequence       uint64
	PreviousDigest []byte
}

// Chain seals and verifies deterministic records. It owns no sequence or key
// persistence and does not claim non-repudiation.
type Chain struct {
	algorithm IntegrityAlgorithm
	keys      KeyProvider
	observer  Observer
}

// Checkpoint binds a partition's durable sequence to its verified digest.
type Checkpoint struct {
	partition string
	sequence  uint64
	digest    []byte
}

// NewCheckpoint validates and defensively copies one durable chain boundary.
func NewCheckpoint(partition string, sequence uint64, digest []byte) (Checkpoint, error) {
	if err := boundedRequired("checkpoint_partition", partition, DefaultLimits().MaxFieldBytes); err != nil {
		return Checkpoint{}, invalid("checkpoint", "must be complete")
	}
	if sequence == 0 || len(digest) != sha256.Size {
		return Checkpoint{}, invalid("checkpoint", "must be complete")
	}
	return Checkpoint{partition: partition, sequence: sequence, digest: append([]byte(nil), digest...)}, nil
}

// Partition returns the stable chain partition.
func (checkpoint Checkpoint) Partition() string { return checkpoint.partition }

// Sequence returns the final verified sequence represented by the checkpoint.
func (checkpoint Checkpoint) Sequence() uint64 { return checkpoint.sequence }

// Digest returns a defensive copy of the final verified digest.
func (checkpoint Checkpoint) Digest() []byte { return append([]byte(nil), checkpoint.digest...) }

// NewChain validates an integrity policy. HMAC requires a KeyProvider; plain
// SHA-256 does not imply authenticity or non-repudiation.
func NewChain(config ChainConfig) (*Chain, error) {
	if config.Algorithm != IntegritySHA256 && config.Algorithm != IntegrityHMACSHA256 {
		return nil, invalid("integrity_algorithm", "must be supported")
	}
	if config.Algorithm == IntegrityHMACSHA256 && config.Keys == nil {
		return nil, invalid("key_provider", "is required for HMAC")
	}
	return &Chain{algorithm: config.Algorithm, keys: config.Keys, observer: config.Observer}, nil
}

// Seal returns a copied record containing the supplied partition and sequence
// plus a deterministic digest. The caller remains responsible for serialized
// durable sequence allocation and checkpoint persistence.
func (chain *Chain) Seal(ctx context.Context, record Record, link ChainLink) (Record, error) {
	if chain == nil || ctx == nil || record.ID() == "" || link.Sequence == 0 {
		return Record{}, invalid("chain_link", "must be complete")
	}
	if err := boundedRequired("chain_partition", link.Partition, DefaultLimits().MaxFieldBytes); err != nil {
		return Record{}, invalid("chain_link", "must be complete")
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if link.Sequence == 1 && len(link.PreviousDigest) != 0 {
		return Record{}, invalid("previous_digest", "must be empty for sequence one")
	}
	if link.Sequence > 1 && len(link.PreviousDigest) != sha256.Size {
		return Record{}, invalid("previous_digest", "must be a SHA-256 digest")
	}
	sealed := record
	sealed.integrity = Integrity{algorithm: chain.algorithm, partition: link.Partition, sequence: link.Sequence, previousDigest: append([]byte(nil), link.PreviousDigest...)}
	key, err := chain.key(ctx, link.Partition, "", record.RecordedAt())
	if err != nil {
		return Record{}, safeKeyFailure(err)
	}
	sealed.integrity.keyID = key.ID
	sealed.integrity.digest = chain.digest(sealed, key.Bytes)
	return sealed, nil
}

// Verify checks a complete single-partition chain beginning at sequence one.
func (chain *Chain) Verify(ctx context.Context, records []Record) error {
	if chain == nil {
		return ErrIntegrityInvalid
	}
	if ctx == nil {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	if len(records) == 0 {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	err := chain.verifyRange(ctx, records[0].integrity.partition, 0, nil, records, Checkpoint{})
	chain.observeInvalid(ctx, err)
	return err
}

// VerifyFromCheckpoint verifies an archived suffix and requires it to end at
// expectedFinal, detecting missing prefixes, links, and truncated exports.
func (chain *Chain) VerifyFromCheckpoint(ctx context.Context, previous Checkpoint, records []Record, expectedFinal Checkpoint) error {
	if chain == nil {
		return ErrIntegrityInvalid
	}
	if ctx == nil {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	if previous.partition == "" {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	if expectedFinal.partition != previous.partition {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	if expectedFinal.sequence < previous.sequence {
		chain.observeInvalid(ctx, ErrIntegrityInvalid)
		return ErrIntegrityInvalid
	}
	err := chain.verifyRange(ctx, previous.partition, previous.sequence, previous.digest, records, expectedFinal)
	chain.observeInvalid(ctx, err)
	return err
}

func (chain *Chain) observeInvalid(ctx context.Context, err error) {
	if errors.Is(err, ErrIntegrityInvalid) {
		safeObserve(ctx, chain.observer, Observation{Kind: ObservationIntegrityInvalid, Count: 1})
	}
}

func (chain *Chain) verifyRange(ctx context.Context, partition string, start uint64, previous []byte, records []Record, final Checkpoint) error {
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		integrity := record.integrity
		if integrity.algorithm != chain.algorithm {
			return ErrIntegrityInvalid
		}
		if integrity.partition != partition {
			return ErrIntegrityInvalid
		}
		if integrity.sequence != start+uint64(index)+1 {
			return ErrIntegrityInvalid
		}
		if !hmac.Equal(integrity.previousDigest, previous) {
			return ErrIntegrityInvalid
		}
		key, err := chain.key(ctx, partition, integrity.keyID, record.RecordedAt())
		if err != nil {
			return safeKeyFailure(err)
		}
		if integrity.keyID != key.ID {
			return ErrIntegrityInvalid
		}
		expected := chain.digest(record, key.Bytes)
		if !hmac.Equal(expected, integrity.digest) {
			return ErrIntegrityInvalid
		}
		previous = integrity.digest
	}
	if final.partition != "" {
		if final.sequence != start+uint64(len(records)) || !hmac.Equal(final.digest, previous) {
			return ErrIntegrityInvalid
		}
	}
	return nil
}

func safeKeyFailure(err error) (result error) {
	defer func() {
		if recover() != nil {
			result = ErrKeyUnavailable
		}
	}()
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrInvalidArgument):
		return ErrInvalidArgument
	default:
		return ErrKeyUnavailable
	}
}

// MerkleRoot returns an order-sensitive deterministic SHA-256 Merkle root over
// canonical record bytes. Odd levels duplicate their final node.
func MerkleRoot(records []Record) ([]byte, error) {
	if len(records) == 0 {
		return nil, invalid("merkle_records", "must not be empty")
	}
	nodes := make([][]byte, len(records))
	for index, record := range records {
		if record.ID() == "" {
			return nil, invalid("merkle_record", "must be valid")
		}
		encoded, _ := CanonicalJSON(record)
		digest := sha256.Sum256(encoded)
		nodes[index] = append([]byte(nil), digest[:]...)
	}
	return merkleLevel(nodes), nil
}

func merkleLevel(nodes [][]byte) []byte {
	switch len(nodes) {
	case 1:
		return nodes[0]
	default:
		var next [][]byte
		for index := 0; index < len(nodes); index = index + 2 {
			payload := []byte{1}
			payload = append(payload, nodes[index]...)
			payload = append(payload, merkleRight(nodes, index)...)
			digest := sha256.Sum256(payload)
			next = append(next, append([]byte(nil), digest[:]...))
		}
		return merkleLevel(next)
	}
}

func merkleRight(nodes [][]byte, index int) []byte {
	if index+1 < len(nodes) {
		return nodes[index+1]
	}
	return nodes[index]
}

func (chain *Chain) key(ctx context.Context, partition, keyID string, recordedAt time.Time) (IntegrityKey, error) {
	switch chain.algorithm {
	case IntegritySHA256:
		return IntegrityKey{}, nil
	}
	key, err := callKeyProvider(chain.keys, ctx, KeyRequest{Partition: partition, KeyID: keyID, RecordedAt: recordedAt})
	if err != nil {
		return IntegrityKey{}, err
	}
	if err := boundedRequired("integrity_key_id", key.ID, DefaultLimits().MaxFieldBytes); err != nil {
		return IntegrityKey{}, invalid("integrity_key", "must have a bounded ID and key")
	}
	if len(key.Bytes) < sha256.Size || len(key.Bytes) > DefaultLimits().MaxFieldBytes {
		return IntegrityKey{}, invalid("integrity_key", "must contain 32 to 1024 bytes")
	}
	key.Bytes = append([]byte(nil), key.Bytes...)
	return key, nil
}

func callKeyProvider(provider KeyProvider, ctx context.Context, request KeyRequest) (key IntegrityKey, err error) {
	defer func() {
		if recover() != nil {
			key = IntegrityKey{}
			err = ErrKeyUnavailable
		}
	}()
	return provider.Key(ctx, request)
}

func (chain *Chain) digest(record Record, key []byte) []byte {
	unsigned := record
	unsigned.integrity.digest = nil
	encoded, _ := CanonicalJSON(unsigned)
	switch chain.algorithm {
	case IntegrityHMACSHA256:
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(encoded)
		return mac.Sum(nil)
	}
	digest := sha256.Sum256(encoded)
	return digest[:]
}
