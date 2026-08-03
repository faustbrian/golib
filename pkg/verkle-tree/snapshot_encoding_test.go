package verkletree_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

const publicSnapshotHeaderBytes = 4 + 1 + 2 + 2 + int(verkletree.RootSize) + 4

func TestPublicSnapshotEncodingIsCanonicalAndSelfAuthenticating(t *testing.T) {
	t.Parallel()

	first := publicKey(0x10, 0x01)
	second := publicKey(0x20, 0x02)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: second, Value: publicValue(2)},
			{Key: first, Value: verkletree.Value{}},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	encodingLimits := publicSnapshotEncodingLimits()
	encoded, err := snapshot.Bytes(context.Background(), encodingLimits)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	if string(encoded[:4]) != "VKSS" {
		t.Fatalf("snapshot magic = %q", encoded[:4])
	}
	if got := binary.BigEndian.Uint32(encoded[publicSnapshotHeaderBytes-4:]); got != 2 {
		t.Fatalf("snapshot entry count = %d, want 2", got)
	}
	if !bytes.Equal(encoded[publicSnapshotHeaderBytes:publicSnapshotHeaderBytes+32], first[:]) {
		t.Fatal("snapshot entries are not canonically key ordered")
	}

	reordered, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: first, Value: verkletree.Value{}},
			{Key: second, Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new reordered snapshot: %v", err)
	}
	reorderedBytes, err := reordered.Bytes(context.Background(), encodingLimits)
	if err != nil {
		t.Fatalf("encode reordered snapshot: %v", err)
	}
	if !bytes.Equal(encoded, reorderedBytes) {
		t.Fatal("snapshot bytes depend on caller entry order")
	}

	decoded, err := verkletree.DecodeSnapshot(
		context.Background(), encoded, publicSnapshotDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	decodedRoot, err := decoded.Root()
	if err != nil {
		t.Fatalf("decoded root: %v", err)
	}
	wantRoot, err := snapshot.Root()
	if err != nil || !equalPublicRoots(t, decodedRoot, wantRoot) {
		t.Fatalf("decoded root mismatch: %v", err)
	}
	value, present, err := decoded.Get(context.Background(), first)
	if err != nil || !present || value != (verkletree.Value{}) {
		t.Fatalf("decoded zero value = %x/%t, error %v", value, present, err)
	}
	reencoded, err := decoded.Bytes(context.Background(), encodingLimits)
	if err != nil || !bytes.Equal(reencoded, encoded) {
		t.Fatalf("reencoded snapshot differs, error %v", err)
	}

	encoded[publicSnapshotHeaderBytes] ^= 0xff
	value, present, err = decoded.Get(context.Background(), first)
	if err != nil || !present || value != (verkletree.Value{}) {
		t.Fatalf("decoded snapshot aliases input = %x/%t, error %v", value, present, err)
	}
}

func TestDecodeSnapshotRejectsMalformedAndExhaustedInput(t *testing.T) {
	t.Parallel()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: publicKey(0x10, 0x01), Value: publicValue(1)},
			{Key: publicKey(0x20, 0x02), Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	encoded, err := snapshot.Bytes(
		context.Background(), publicSnapshotEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	limits := publicSnapshotDecodingLimits()

	tests := map[string]struct {
		encoded []byte
		limits  verkletree.SnapshotDecodingLimits
		want    error
	}{
		"wrong magic": {
			encoded: mutatePublicSnapshotByte(encoded, 0, 'X'),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"truncated": {
			encoded: append([]byte(nil), encoded[:len(encoded)-1]...),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"trailing": {
			encoded: append(append([]byte(nil), encoded...), 0),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"wrong profile": {
			encoded: mutatePublicSnapshotByte(encoded, 4, 0xff),
			limits:  limits,
			want:    verkletree.ErrUnsupportedProfile,
		},
		"wrong profile version": {
			encoded: mutatePublicSnapshotByte(encoded, 6, 1),
			limits:  limits,
			want:    verkletree.ErrUnsupportedProfile,
		},
		"wrong container version": {
			encoded: mutatePublicSnapshotByte(encoded, 8, 2),
			limits:  limits,
			want:    verkletree.ErrUnsupportedProfile,
		},
		"changed root": {
			encoded: mutatePublicSnapshotByte(encoded, 10, encoded[10]^0x01),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"reordered entries": {
			encoded: reversePublicSnapshotEntries(encoded),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"duplicate entries": {
			encoded: duplicateFirstPublicSnapshotEntry(encoded),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"wrong count": {
			encoded: mutatePublicSnapshotCount(encoded, 3),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
		"entry budget": {
			encoded: encoded,
			limits: func() verkletree.SnapshotDecodingLimits {
				value := limits
				value.MaxEntries = 1
				return value
			}(),
			want: verkletree.ErrResourceExhausted,
		},
		"byte budget": {
			encoded: encoded,
			limits: func() verkletree.SnapshotDecodingLimits {
				value := limits
				value.MaxSnapshotBytes = uint64(len(encoded) - 1)
				return value
			}(),
			want: verkletree.ErrResourceExhausted,
		},
		"temporary budget": {
			encoded: encoded,
			limits: func() verkletree.SnapshotDecodingLimits {
				value := limits
				value.MaxTemporaryBytes = 1
				return value
			}(),
			want: verkletree.ErrResourceExhausted,
		},
		"point budget": {
			encoded: encoded,
			limits: func() verkletree.SnapshotDecodingLimits {
				value := limits
				value.MaxPointDecodes = 0
				return value
			}(),
			want: verkletree.ErrResourceExhausted,
		},
		"nested state budget": {
			encoded: encoded,
			limits: func() verkletree.SnapshotDecodingLimits {
				value := limits
				value.Snapshot.State.MaxEntries = 1
				return value
			}(),
			want: verkletree.ErrResourceExhausted,
		},
		"different valid root": {
			encoded: snapshotWithDifferentValidRoot(t, encoded),
			limits:  limits,
			want:    verkletree.ErrInvalidSnapshot,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, decodeErr := verkletree.DecodeSnapshot(
				context.Background(), test.encoded, test.limits,
			)
			if !errors.Is(decodeErr, test.want) {
				t.Fatalf("decode error = %v, want %v", decodeErr, test.want)
			}
		})
	}
}

func TestSnapshotEncodingRejectsInvalidUseAndOwnsEmptyState(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	var zero verkletree.Snapshot
	if _, err := zero.Bytes(
		context.Background(), publicSnapshotEncodingLimits(),
	); !errors.Is(err, verkletree.ErrInvalidSnapshot) {
		t.Fatalf("zero snapshot encode error = %v", err)
	}
	empty, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		nil,
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new empty snapshot: %v", err)
	}
	if _, err := empty.Bytes(
		nilContext, publicSnapshotEncodingLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil-context encode error = %v", err)
	}
	if _, err := empty.Bytes(
		context.Background(), verkletree.SnapshotEncodingLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("zero encoding limits error = %v", err)
	}
	excessiveEncoding := publicSnapshotEncodingLimits()
	excessiveEncoding.MaxSnapshotBytes = ^uint64(0)
	if _, err := empty.Bytes(
		context.Background(), excessiveEncoding,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive encoding limits error = %v", err)
	}
	encoded, err := empty.Bytes(
		context.Background(), publicSnapshotEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode empty snapshot: %v", err)
	}
	decodingLimits := publicSnapshotDecodingLimits()
	decodingLimits.MaxPointDecodes = 0
	decoded, err := verkletree.DecodeSnapshot(
		context.Background(), encoded, decodingLimits,
	)
	if err != nil {
		t.Fatalf("decode empty snapshot without point work: %v", err)
	}
	root, err := decoded.Root()
	if err != nil {
		t.Fatalf("decoded empty root: %v", err)
	}
	isEmpty, err := root.IsEmpty()
	if err != nil || !isEmpty {
		t.Fatalf("decoded empty root = %t, error %v", isEmpty, err)
	}

	if _, err := verkletree.DecodeSnapshot(
		nilContext, encoded, decodingLimits,
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil-context decode error = %v", err)
	}
	if _, err := verkletree.DecodeSnapshot(
		context.Background(), encoded, verkletree.SnapshotDecodingLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("zero decoding limits error = %v", err)
	}
	excessive := decodingLimits
	excessive.MaxSnapshotBytes = ^uint64(0)
	if _, err := verkletree.DecodeSnapshot(
		context.Background(), encoded, excessive,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive decoding limits error = %v", err)
	}
	excessive = publicSnapshotDecodingLimits()
	excessive.MaxPointDecodes = 2
	if _, err := verkletree.DecodeSnapshot(
		context.Background(), encoded, excessive,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive point limit error = %v", err)
	}
}

func TestSnapshotEncodingPreflightsEveryAllocation(t *testing.T) {
	t.Parallel()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: publicKey(0x10, 0x01), Value: publicValue(1)},
			{Key: publicKey(0x20, 0x02), Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	valid := publicSnapshotEncodingLimits()
	tests := map[string]struct {
		mutate   func(*verkletree.SnapshotEncodingLimits)
		resource verkletree.Resource
	}{
		"entries": {
			mutate: func(value *verkletree.SnapshotEncodingLimits) {
				value.MaxEntries = 1
			},
			resource: verkletree.ResourceEntries,
		},
		"bytes": {
			mutate: func(value *verkletree.SnapshotEncodingLimits) {
				value.MaxSnapshotBytes = uint64(publicSnapshotHeaderBytes + 2*64 - 1)
			},
			resource: verkletree.ResourceSnapshotBytes,
		},
		"temporary memory": {
			mutate: func(value *verkletree.SnapshotEncodingLimits) {
				value.MaxTemporaryBytes = uint64(publicSnapshotHeaderBytes + 4*64 - 1)
			},
			resource: verkletree.ResourceTemporaryBytes,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := valid
			test.mutate(&limits)
			_, encodeErr := snapshot.Bytes(context.Background(), limits)
			var resourceErr *verkletree.ResourceError
			if !errors.As(encodeErr, &resourceErr) ||
				resourceErr.Resource != test.resource {
				t.Fatalf("encode error = %v, want resource %d", encodeErr, test.resource)
			}
		})
	}
}

func BenchmarkEncodeSnapshotTwoEntries(b *testing.B) {
	snapshot := benchmarkPublicSnapshot(b)
	limits := publicSnapshotEncodingLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := snapshot.Bytes(context.Background(), limits); err != nil {
			b.Fatalf("encode snapshot: %v", err)
		}
	}
}

func BenchmarkDecodeSnapshotTwoEntries(b *testing.B) {
	snapshot := benchmarkPublicSnapshot(b)
	encoded, err := snapshot.Bytes(
		context.Background(), publicSnapshotEncodingLimits(),
	)
	if err != nil {
		b.Fatalf("encode snapshot: %v", err)
	}
	limits := publicSnapshotDecodingLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := verkletree.DecodeSnapshot(
			context.Background(), encoded, limits,
		); err != nil {
			b.Fatalf("decode snapshot: %v", err)
		}
	}
}

func benchmarkPublicSnapshot(tb testing.TB) verkletree.Snapshot {
	tb.Helper()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: publicKey(0x10, 0x01), Value: publicValue(1)},
			{Key: publicKey(0x20, 0x02), Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		tb.Fatalf("new snapshot: %v", err)
	}

	return snapshot
}

func publicSnapshotEncodingLimits() verkletree.SnapshotEncodingLimits {
	return verkletree.SnapshotEncodingLimits{
		MaxSnapshotBytes:  4 << 10,
		MaxEntries:        8,
		MaxTemporaryBytes: 8 << 10,
	}
}

func publicSnapshotDecodingLimits() verkletree.SnapshotDecodingLimits {
	return verkletree.SnapshotDecodingLimits{
		MaxSnapshotBytes:  4 << 10,
		MaxEntries:        8,
		MaxPointDecodes:   1,
		MaxTemporaryBytes: 8 << 10,
		Snapshot:          publicSnapshotLimits(),
	}
}

func mutatePublicSnapshotByte(encoded []byte, index int, value byte) []byte {
	mutated := append([]byte(nil), encoded...)
	mutated[index] = value

	return mutated
}

func reversePublicSnapshotEntries(encoded []byte) []byte {
	mutated := append([]byte(nil), encoded...)
	first := append([]byte(nil), mutated[publicSnapshotHeaderBytes:publicSnapshotHeaderBytes+64]...)
	copy(
		mutated[publicSnapshotHeaderBytes:publicSnapshotHeaderBytes+64],
		mutated[publicSnapshotHeaderBytes+64:publicSnapshotHeaderBytes+128],
	)
	copy(mutated[publicSnapshotHeaderBytes+64:], first)

	return mutated
}

func duplicateFirstPublicSnapshotEntry(encoded []byte) []byte {
	mutated := append([]byte(nil), encoded...)
	copy(
		mutated[publicSnapshotHeaderBytes+64:publicSnapshotHeaderBytes+128],
		mutated[publicSnapshotHeaderBytes:publicSnapshotHeaderBytes+64],
	)

	return mutated
}

func mutatePublicSnapshotCount(encoded []byte, count uint32) []byte {
	mutated := append([]byte(nil), encoded...)
	binary.BigEndian.PutUint32(
		mutated[publicSnapshotHeaderBytes-4:publicSnapshotHeaderBytes], count,
	)

	return mutated
}

func snapshotWithDifferentValidRoot(t testing.TB, encoded []byte) []byte {
	t.Helper()

	different, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{
			Key: publicKey(0x30, 0x03), Value: publicValue(3),
		}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new different snapshot: %v", err)
	}
	differentBytes, err := different.Bytes(
		context.Background(), publicSnapshotEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode different snapshot: %v", err)
	}
	mutated := append([]byte(nil), encoded...)
	copy(mutated[9:publicSnapshotHeaderBytes-4], differentBytes[9:publicSnapshotHeaderBytes-4])

	return mutated
}
