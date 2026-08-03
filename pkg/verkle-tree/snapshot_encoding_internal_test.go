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
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		translated := translateSnapshotEncodingError("copy", cause)
		if !errors.Is(translated, ErrCancelled) || !errors.Is(translated, cause) {
			t.Fatalf("encoding cancellation %v translated to %v", cause, translated)
		}
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
	for _, cause := range []error{
		ErrCancelled,
		context.Canceled,
		context.DeadlineExceeded,
	} {
		translated := translateSnapshotDecodingError("decode", cause)
		if !errors.Is(translated, ErrCancelled) || !errors.Is(translated, cause) {
			t.Fatalf("decoding cancellation %v translated to %v", cause, translated)
		}
	}
	if err := checkSnapshotEncodingResource(ResourceEntries, 7, 7); err != nil {
		t.Fatalf("exact encoding resource error = %v", err)
	}
	var excessiveResource *ResourceError
	if err := checkSnapshotEncodingResource(ResourceEntries, 7, 8); !errors.As(err, &excessiveResource) ||
		excessiveResource.Resource != ResourceEntries {
		t.Fatalf("excessive encoding resource error = %v", err)
	}

	corrupt := Snapshot{valid: true}
	if _, err := corrupt.Bytes(context.Background(), encodingLimits); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("corrupt snapshot encoding error = %v", err)
	}
}

func TestSnapshotEncodingLimitCeilings(t *testing.T) {
	t.Parallel()

	safeEntries := uint32(
		((int64(1) << 31) - 1 - int64(snapshotHeaderBytes)) /
			int64(snapshotEntryBytes),
	)
	safeBytes := uint64(snapshotHeaderBytes) +
		uint64(safeEntries)*uint64(snapshotEntryBytes)
	limits := SnapshotEncodingLimits{
		MaxSnapshotBytes:  safeBytes,
		MaxEntries:        safeEntries,
		MaxTemporaryBytes: 1,
	}
	if err := limits.validate(); err != nil {
		t.Fatalf("safe encoding ceilings error = %v", err)
	}

	excessiveEntries := limits
	excessiveEntries.MaxEntries++
	if err := excessiveEntries.validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("excessive entry ceiling error = %v", err)
	}

	excessiveBytes := limits
	excessiveBytes.MaxSnapshotBytes++
	if err := excessiveBytes.validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("excessive byte ceiling error = %v", err)
	}

	decodingLimits := SnapshotDecodingLimits{
		MaxSnapshotBytes:  safeBytes,
		MaxEntries:        safeEntries,
		MaxPointDecodes:   1,
		MaxTemporaryBytes: 1,
		Snapshot:          testFacadeSnapshotLimits(),
	}
	if err := decodingLimits.validate(); err != nil {
		t.Fatalf("safe decoding ceilings error = %v", err)
	}

	excessiveDecodingEntries := decodingLimits
	excessiveDecodingEntries.MaxEntries++
	if err := excessiveDecodingEntries.validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("excessive decoding entry ceiling error = %v", err)
	}

	excessiveDecodingBytes := decodingLimits
	excessiveDecodingBytes.MaxSnapshotBytes++
	if err := excessiveDecodingBytes.validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("excessive decoding byte ceiling error = %v", err)
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
