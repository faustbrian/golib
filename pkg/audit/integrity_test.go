package audit_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestChainSealsAndVerifiesPartitionedRecordsAcrossKeyRotation(t *testing.T) {
	t.Parallel()

	invalidObservations := 0
	chain, err := audit.NewChain(audit.ChainConfig{
		Algorithm: audit.IntegrityHMACSHA256,
		Observer: audit.ObserverFunc(func(_ context.Context, observation audit.Observation) {
			if observation.Kind == audit.ObservationIntegrityInvalid {
				invalidObservations++
			}
		}),
		Keys: audit.KeyProviderFunc(func(_ context.Context, partition string, recordedAt time.Time) (audit.IntegrityKey, error) {
			if partition != "tenant-1" {
				return audit.IntegrityKey{}, errors.New("unexpected partition")
			}
			if recordedAt.Before(time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)) {
				return audit.IntegrityKey{ID: "key-1", Bytes: []byte("0123456789abcdef0123456789abcdef")}, nil
			}
			return audit.IntegrityKey{ID: "key-2", Bytes: []byte("abcdef0123456789abcdef0123456789")}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	first := integrityRecord(t, "record-1", time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC))
	first, err = chain.Seal(context.Background(), first, audit.ChainLink{Partition: "tenant-1", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	second := integrityRecord(t, "record-2", time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC))
	second, err = chain.Seal(context.Background(), second, audit.ChainLink{
		Partition:      "tenant-1",
		Sequence:       2,
		PreviousDigest: first.Integrity().Digest(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.Integrity().KeyID() != "key-1" || second.Integrity().KeyID() != "key-2" {
		t.Fatalf("key rotation metadata = %q, %q", first.Integrity().KeyID(), second.Integrity().KeyID())
	}
	if err := chain.Verify(context.Background(), []audit.Record{first, second}); err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(context.Background(), []audit.Record{second, first}); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("out-of-order chain verification error = %v", err)
	}
	if invalidObservations != 1 {
		t.Fatalf("integrity-invalid observations = %d", invalidObservations)
	}
}

func TestCheckpointAndMerkleVerificationDetectTruncationAndOrder(t *testing.T) {
	t.Parallel()
	chain, err := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	first, err := chain.Seal(context.Background(), integrityRecord(t, "record-1", base), audit.ChainLink{Partition: "tenant-1", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := audit.NewCheckpoint("tenant-1", 1, first.Integrity().Digest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := chain.Seal(context.Background(), integrityRecord(t, "record-2", base.Add(time.Second)), audit.ChainLink{
		Partition: "tenant-1", Sequence: 2, PreviousDigest: checkpoint.Digest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := audit.NewCheckpoint("tenant-1", 2, second.Integrity().Digest())
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.VerifyFromCheckpoint(context.Background(), checkpoint, []audit.Record{second}, final); err != nil {
		t.Fatal(err)
	}
	if err := chain.VerifyFromCheckpoint(context.Background(), checkpoint, nil, final); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("truncated suffix verification error = %v", err)
	}

	root, err := audit.MerkleRoot([]audit.Record{first, second})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := audit.CanonicalJSON(first)
	secondJSON, _ := audit.CanonicalJSON(second)
	firstLeaf, secondLeaf := sha256.Sum256(firstJSON), sha256.Sum256(secondJSON)
	joined := append([]byte{1}, firstLeaf[:]...)
	joined = append(joined, secondLeaf[:]...)
	want := sha256.Sum256(joined)
	if string(root) != string(want[:]) {
		t.Fatalf("MerkleRoot() = %x, want %x", root, want)
	}
	reversed, err := audit.MerkleRoot([]audit.Record{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if string(root) == string(reversed) {
		t.Fatal("Merkle root ignored stable export order")
	}
}

func TestIntegrityContractsRejectIncompleteKeysLinksAndCheckpoints(t *testing.T) {
	t.Parallel()

	for _, config := range []audit.ChainConfig{
		{},
		{Algorithm: audit.IntegrityAlgorithm(99)},
		{Algorithm: audit.IntegrityHMACSHA256},
	} {
		if _, err := audit.NewChain(config); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewChain(%#v) error = %v", config, err)
		}
	}
	for _, input := range []struct {
		partition string
		sequence  uint64
		digest    []byte
	}{
		{"", 1, make([]byte, sha256.Size)},
		{"tenant-1", 0, make([]byte, sha256.Size)},
		{"tenant-1", 1, []byte{1}},
		{string([]byte{0xff}), 1, make([]byte, sha256.Size)},
		{strings.Repeat("p", audit.DefaultLimits().MaxFieldBytes+1), 1, make([]byte, sha256.Size)},
	} {
		if _, err := audit.NewCheckpoint(input.partition, input.sequence, input.digest); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewCheckpoint(%#v) error = %v", input, err)
		}
	}
	digest := make([]byte, sha256.Size)
	checkpoint, err := audit.NewCheckpoint("tenant-1", 1, digest)
	if err != nil {
		t.Fatal(err)
	}
	digest[0] = 1
	if checkpoint.Partition() != "tenant-1" || checkpoint.Sequence() != 1 || checkpoint.Digest()[0] != 0 {
		t.Fatal("checkpoint did not defensively own its digest")
	}

	chain, _ := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	record := integrityRecord(t, "record-1", time.Now())
	for _, link := range []audit.ChainLink{
		{},
		{Partition: "tenant-1"},
		{Partition: "tenant-1", Sequence: 1, PreviousDigest: make([]byte, sha256.Size)},
		{Partition: "tenant-1", Sequence: 2},
		{Partition: string([]byte{0xff}), Sequence: 1},
		{Partition: strings.Repeat("p", audit.DefaultLimits().MaxFieldBytes+1), Sequence: 1},
	} {
		if _, err := chain.Seal(context.Background(), record, link); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("Seal(%#v) error = %v", link, err)
		}
	}
	var nilChain *audit.Chain
	if _, err := nilChain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil Chain.Seal() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.Seal(canceled, record, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Seal() error = %v", err)
	}
	if _, err := audit.MerkleRoot(nil); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("MerkleRoot(nil) error = %v", err)
	}
	if _, err := chain.Seal(context.Background(), audit.Record{}, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("Seal(zero record) error = %v", err)
	}
	if _, err := audit.MerkleRoot([]audit.Record{{}}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("MerkleRoot(zero record) error = %v", err)
	}
}

func TestIntegrityKeyFailuresAreBoundedAndInspectable(t *testing.T) {
	t.Parallel()
	var nilProvider audit.KeyProviderFunc
	if _, err := nilProvider.Key(context.Background(), "tenant-1", time.Now()); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil KeyProviderFunc error = %v", err)
	}

	record := integrityRecord(t, "record-1", time.Now())
	providerFailure := errors.New("KMS unavailable")
	chain, _ := audit.NewChain(audit.ChainConfig{
		Algorithm: audit.IntegrityHMACSHA256,
		Keys: audit.KeyProviderFunc(func(context.Context, string, time.Time) (audit.IntegrityKey, error) {
			return audit.IntegrityKey{}, providerFailure
		}),
	})
	if _, err := chain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); !errors.Is(err, providerFailure) {
		t.Fatalf("provider-failed Seal() error = %v", err)
	}
	for _, key := range []audit.IntegrityKey{
		{Bytes: make([]byte, sha256.Size)},
		{ID: "key-1", Bytes: []byte("short")},
		{ID: string([]byte{0xff}), Bytes: make([]byte, sha256.Size)},
		{ID: "key-1", Bytes: make([]byte, audit.DefaultLimits().MaxFieldBytes+1)},
	} {
		key := key
		chain, _ := audit.NewChain(audit.ChainConfig{
			Algorithm: audit.IntegrityHMACSHA256,
			Keys:      audit.KeyProviderFunc(func(context.Context, string, time.Time) (audit.IntegrityKey, error) { return key, nil }),
		})
		if _, err := chain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("invalid-key Seal() error = %v", err)
		}
	}
	boundaryChain, _ := audit.NewChain(audit.ChainConfig{
		Algorithm: audit.IntegrityHMACSHA256,
		Keys: audit.KeyProviderFunc(func(context.Context, string, time.Time) (audit.IntegrityKey, error) {
			return audit.IntegrityKey{ID: "key-boundary", Bytes: make([]byte, audit.DefaultLimits().MaxFieldBytes)}, nil
		}),
	})
	if _, err := boundaryChain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1}); err != nil {
		t.Fatalf("exact-boundary key Seal() error = %v", err)
	}
}

func TestIntegrityInvalidInputsEmitSafeObservation(t *testing.T) {
	t.Parallel()

	observed := 0
	chain, err := audit.NewChain(audit.ChainConfig{
		Algorithm: audit.IntegritySHA256,
		Observer: audit.ObserverFunc(func(_ context.Context, value audit.Observation) {
			if value.Kind == audit.ObservationIntegrityInvalid {
				observed++
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(context.Background(), nil); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("Verify(nil) error = %v", err)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if err := chain.Verify(nil, []audit.Record{integrityRecord(t, "record-2", time.Now())}); !errors.Is(err, audit.ErrIntegrityInvalid) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("Verify(nil context) error = %v", err)
	}
	if err := chain.VerifyFromCheckpoint(context.Background(), audit.Checkpoint{}, nil, audit.Checkpoint{}); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("VerifyFromCheckpoint(zero) error = %v", err)
	}
	digest := make([]byte, sha256.Size)
	previous, _ := audit.NewCheckpoint("tenant-1", 2, digest)
	wrongPartition, _ := audit.NewCheckpoint("tenant-2", 2, digest)
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if err := chain.VerifyFromCheckpoint(nil, previous, nil, previous); !errors.Is(err, audit.ErrIntegrityInvalid) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("VerifyFromCheckpoint(nil context) error = %v", err)
	}
	if err := chain.VerifyFromCheckpoint(context.Background(), previous, nil, wrongPartition); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("VerifyFromCheckpoint(partition mismatch) error = %v", err)
	}
	if observed != 5 {
		t.Fatalf("integrity-invalid observations = %d", observed)
	}
}

func integrityRecord(t *testing.T, id string, recordedAt time.Time) audit.Record {
	t.Helper()
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return recordedAt },
		IDGenerator: func() (string, error) { return id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: recordedAt,
		Action:     "account.updated",
		Outcome:    audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorSystem, ID: "billing"},
		Subject:    audit.SubjectInput{Type: "account", ID: "account-1"},
		Changes:    audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
