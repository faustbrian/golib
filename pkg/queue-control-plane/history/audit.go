// Package history defines bounded operational and append-only audit contracts.
package history

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrAuditTampered indicates that an entry's content or chain position does
// not match its stored digest.
var ErrAuditTampered = errors.New("history: audit chain verification failed")

// Hash is an audit-chain SHA-256 digest.
type Hash [sha256.Size]byte

// Event is the canonical content of one administrative audit record.
type Event struct {
	Sequence       uint64
	HashVersion    uint16
	OccurredAt     time.Time
	CommandID      string
	IdempotencyKey string
	Actor          string
	Action         string
	Target         string
	Result         string
}

// Entry links an event to the preceding retained or anchored audit digest.
type Entry struct {
	PreviousHash Hash
	Hash         Hash
	Event        Event
}

// HashBytes creates an anchor hash for a retained prefix or external root.
func HashBytes(value []byte) Hash {
	return sha256.Sum256(value)
}

// Seal returns an immutable-value audit entry linked to previous.
func Seal(previous Hash, event Event) Entry {
	return Entry{
		PreviousHash: previous,
		Hash:         digest(previous, event),
		Event:        event,
	}
}

// Verify checks the content and ordering of entries after anchor. An empty
// retained page is valid because there is no content to contradict its anchor.
func Verify(anchor Hash, entries []Entry) error {
	return verifyHashes(anchor, entries)
}

// VerifyFrom checks hash and contiguous sequence integrity after an anchor.
func VerifyFrom(anchorSequence uint64, anchor Hash, entries []Entry) error {
	expected := anchorSequence
	for _, entry := range entries {
		if expected == math.MaxUint64 {
			return fmt.Errorf("%w after sequence %d", ErrAuditTampered, expected)
		}
		expected++
		if entry.Event.Sequence != expected {
			return fmt.Errorf("%w at sequence %d", ErrAuditTampered, entry.Event.Sequence)
		}
	}

	return verifyHashes(anchor, entries)
}

func verifyHashes(anchor Hash, entries []Entry) error {
	previous := anchor
	for _, entry := range entries {
		if entry.Event.HashVersion > 2 {
			return fmt.Errorf("%w at sequence %d", ErrAuditTampered, entry.Event.Sequence)
		}
		if entry.PreviousHash != previous || entry.Hash != digest(previous, entry.Event) {
			return fmt.Errorf("%w at sequence %d", ErrAuditTampered, entry.Event.Sequence)
		}

		previous = entry.Hash
	}

	return nil
}

func digest(previous Hash, event Event) Hash {
	var storage [2_048]byte
	encoded := storage[:0]
	if event.HashVersion == 2 {
		encoded = append(encoded, "go-queue-control-plane/audit/v2"...)
	} else {
		encoded = append(encoded, "go-queue-control-plane/audit/v1"...)
	}
	encoded = append(encoded, previous[:]...)
	encoded = binary.BigEndian.AppendUint64(encoded, event.Sequence)
	encoded = binary.BigEndian.AppendUint64(encoded, uint64(event.OccurredAt.UnixNano()))
	if event.HashVersion == 2 {
		encoded = appendString(encoded, event.CommandID)
	}
	encoded = appendString(encoded, event.IdempotencyKey)
	encoded = appendString(encoded, event.Actor)
	encoded = appendString(encoded, event.Action)
	encoded = appendString(encoded, event.Target)
	encoded = appendString(encoded, event.Result)

	return sha256.Sum256(encoded)
}

func appendString(target []byte, value string) []byte {
	target = binary.BigEndian.AppendUint64(target, uint64(len(value)))

	return append(target, value...)
}
