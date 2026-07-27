package postgres

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func recordDigest(key idempotency.Key) []byte {
	return RecordKeyDigest(key)
}

// RecordKeyDigest returns the opaque table-key digest used by this adapter.
//
// The returned slice is newly allocated and may be retained or mutated by the
// caller. Business transactions may use it to lock and validate the same
// idempotency row before applying a fenced side effect.
func RecordKeyDigest(key idempotency.Key) []byte {
	digest := sha256.New()
	writeDigestPart(digest, key.Namespace())
	writeDigestPart(digest, key.Tenant())
	writeDigestPart(digest, key.Operation())
	writeDigestPart(digest, key.Caller())
	writeDigestPart(digest, key.Value())
	return digest.Sum(nil)
}

func advisoryLockKey(digest []byte) int64 {
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

func writeDigestPart(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}
