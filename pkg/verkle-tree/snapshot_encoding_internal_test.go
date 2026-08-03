package verkletree

import (
	"context"
	"errors"
	"testing"
)

func TestSnapshotEncodingCancellationAndErrorClassification(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		[]Entry{
			{Key: Key{0x10}, Value: Value{1}},
			{Key: Key{0x20}, Value: Value{2}},
		},
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	encodingLimits := SnapshotEncodingLimits{
		MaxSnapshotBytes:  4096,
		MaxEntries:        8,
		MaxTemporaryBytes: 8192,
	}
	encoded, err := snapshot.Bytes(context.Background(), encodingLimits)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	decodingLimits := SnapshotDecodingLimits{
		MaxSnapshotBytes:  4096,
		MaxEntries:        8,
		MaxPointDecodes:   1,
		MaxTemporaryBytes: 8192,
		Snapshot:          testFacadeSnapshotLimits(),
	}

	assertSnapshotCancellationSweep(t, 32, func(ctx context.Context) error {
		_, encodeErr := snapshot.Bytes(ctx, encodingLimits)

		return encodeErr
	})
	assertSnapshotCancellationSweep(t, 4096, func(ctx context.Context) error {
		_, decodeErr := DecodeSnapshot(ctx, encoded, decodingLimits)

		return decodeErr
	})

	if err := translateSnapshotEncodingError("copy", errors.New("fault")); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("encoding fallback error = %v", err)
	}
	resource := newPublicResourceError(ResourceEntries, 1, 2)
	if got := translateSnapshotDecodingError("decode", resource); got != resource {
		t.Fatalf("decoding resource error = %v", got)
	}
	if err := translateSnapshotDecodingError("decode", ErrUnsupportedProfile); !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("decoding profile error = %v", err)
	}
	if err := translateSnapshotDecodingError("decode", errors.New("fault")); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("decoding fallback error = %v", err)
	}

	corrupt := Snapshot{valid: true}
	if _, err := corrupt.Bytes(context.Background(), encodingLimits); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("corrupt snapshot encoding error = %v", err)
	}
}

func assertSnapshotCancellationSweep(
	t *testing.T,
	maximum int,
	operation func(context.Context) error,
) {
	t.Helper()

	for successfulChecks := 0; successfulChecks < maximum; successfulChecks++ {
		err := operation(&cancellingContext{remaining: successfulChecks})
		if err == nil {
			return
		}
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf(
				"cancellation after %d successful checks = %v",
				successfulChecks,
				err,
			)
		}
	}

	t.Fatalf("cancellation sweep did not reach success within %d checks", maximum)
}
