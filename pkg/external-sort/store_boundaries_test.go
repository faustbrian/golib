package externalsort

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestStoreAllocatesExactlyTheDeclaredRecordBuffer(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 3, 4, 4))
	if capacity := cap(store.buffer.data); capacity != 12 {
		t.Fatalf("record buffer capacity = %d, want 12", capacity)
	}
}

func TestStoreValidityRequiresEveryOwnedDependency(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 1, 1, 1))
	if !store.valid() {
		t.Fatal("freshly opened store is invalid")
	}
	var nilStore *Store
	if nilStore.valid() || (&Store{}).valid() {
		t.Fatal("nil-state store is valid")
	}

	testCases := map[string]func(*Store){
		"config": func(candidate *Store) {
			candidate.config.RecordBytes = 0
		},
		"name entropy": func(candidate *Store) {
			candidate.nameEntropy = nil
		},
		"create temporary file": func(candidate *Store) {
			candidate.createTemp = nil
		},
		"open file": func(candidate *Store) {
			candidate.openFile = nil
		},
		"remove file": func(candidate *Store) {
			candidate.remove = nil
		},
		"remove directory": func(candidate *Store) {
			candidate.removeAll = nil
		},
		"root": func(candidate *Store) {
			candidate.root = nil
		},
		"directory": func(candidate *Store) {
			candidate.directory = ""
		},
		"directory name": func(candidate *Store) {
			candidate.directoryName = ""
		},
		"cipher": func(candidate *Store) {
			candidate.cipher = nil
		},
		"identity": func(candidate *Store) {
			candidate.identity = nil
		},
	}
	for name, invalidate := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := openTestStore(
				t,
				newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
			)
			invalidate(candidate)
			if candidate.valid() {
				t.Fatal("store remained valid without its required dependency")
			}
		})
	}

	invalid := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	invalid.createTemp = nil
	if err := invalid.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("Add() error = %v, want invalid configuration", err)
	}
}

func TestConfigurationBoundariesAreExact(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	testCases := map[string]struct {
		config Config
		valid  bool
	}{
		"maximum record bytes": {
			config: Config{parent, MaximumRecordBytes, 1, 1},
			valid:  true,
		},
		"record bytes above maximum": {
			config: Config{parent, MaximumRecordBytes + 1, 1, 1},
		},
		"maximum chunk records": {
			config: Config{parent, 1, MaximumChunkRecords, MaximumChunkRecords},
			valid:  true,
		},
		"chunk records above maximum": {
			config: Config{parent, 1, MaximumChunkRecords + 1, MaximumChunkRecords + 1},
		},
		"maximum chunk bytes": {
			config: Config{parent, MaximumRecordBytes, MaximumChunkBytes / MaximumRecordBytes, MaximumChunkBytes / MaximumRecordBytes},
			valid:  true,
		},
		"chunk bytes above maximum": {
			config: Config{parent, MaximumRecordBytes, MaximumChunkBytes/MaximumRecordBytes + 1, MaximumChunkBytes/MaximumRecordBytes + 1},
		},
		"maximum exact merge fan-in": {
			config: Config{parent, 1, 2, MaximumMergeFiles * 2},
			valid:  true,
		},
		"maximum rounded merge fan-in": {
			config: Config{parent, 1, 2, MaximumMergeFiles*2 - 1},
			valid:  true,
		},
		"merge fan-in above maximum": {
			config: Config{parent, 1, 2, MaximumMergeFiles*2 + 1},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if actual := validConfig(testCase.config); actual != testCase.valid {
				t.Fatalf("validConfig() = %t, want %t", actual, testCase.valid)
			}
		})
	}
}

func TestMergeSkipsEmptyReadersAndTerminatesAfterTheLastRecord(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 1, 2, 2))
	addTestRecords(t, store, []byte{2}, []byte{1})
	readers, err := store.openReaders()
	if err != nil {
		t.Fatalf("openReaders() error = %v", err)
	}
	t.Cleanup(func() { _ = closeReaders(readers) })

	empty := &chunkReader{
		file:        &fakeChunkFile{reader: bytes.NewReader(nil)},
		cipher:      store.cipher,
		recordBytes: store.config.RecordBytes,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var output [][]byte
	if err := merge(
		ctx,
		append([]*chunkReader{empty}, readers...),
		func(record []byte) error {
			output = append(output, bytes.Clone(record))
			if len(output) == 2 {
				cancel()
			}

			return nil
		},
	); err != nil {
		t.Fatalf("merge() error = %v", err)
	}
	want := [][]byte{{1}, {2}}
	if !equalRecords(output, want) {
		t.Fatalf("merged records = %v, want %v", output, want)
	}
}

func TestChunkReaderRejectsAmbiguousEOFAndWrongPlaintextSize(t *testing.T) {
	t.Parallel()

	t.Run("storage read error", func(t *testing.T) {
		t.Parallel()

		reader := &chunkReader{
			file: &fakeChunkFile{
				reader: errorReader{err: errors.New("read failed")},
			},
			cipher:          fixedAEAD{plaintext: []byte{1}},
			recordBytes:     1,
			expectedRecords: 1,
		}
		if _, err := reader.next(); !errors.Is(err, ErrStorage) {
			t.Fatalf("next() error = %v, want storage", err)
		}
	})

	t.Run("zero byte non-EOF error", func(t *testing.T) {
		t.Parallel()

		reader := &chunkReader{
			file: &fakeChunkFile{
				reader: errorReader{err: io.ErrUnexpectedEOF},
			},
			cipher:          fixedAEAD{plaintext: []byte{1}},
			recordBytes:     1,
			expectedRecords: 1,
		}
		if _, err := reader.next(); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("next() error = %v, want corrupt", err)
		}
	})

	t.Run("wrong plaintext size", func(t *testing.T) {
		t.Parallel()

		reader := &chunkReader{
			file: &fakeChunkFile{
				reader: bytes.NewReader(make([]byte, 4)),
			},
			cipher:          fixedAEAD{plaintext: []byte{1}},
			recordBytes:     2,
			expectedRecords: 1,
		}
		if _, err := reader.next(); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("next() error = %v, want corrupt", err)
		}
	})

	t.Run("expected boundary storage read error", func(t *testing.T) {
		t.Parallel()

		reader := &chunkReader{
			file: &fakeChunkFile{
				reader: errorReader{err: errors.New("read failed")},
			},
			cipher: fixedAEAD{},
		}
		if _, err := reader.next(); !errors.Is(err, ErrStorage) {
			t.Fatalf("next() error = %v, want storage", err)
		}
	})

	t.Run("trailing byte with read error", func(t *testing.T) {
		t.Parallel()

		reader := &chunkReader{
			file: &fakeChunkFile{
				reader: dataErrorReader{err: errors.New("read failed")},
			},
			cipher: fixedAEAD{},
		}
		if _, err := reader.next(); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("next() error = %v, want corrupt", err)
		}
	})
}

type dataErrorReader struct {
	err error
}

func (reader dataErrorReader) Read(destination []byte) (int, error) {
	destination[0] = 1

	return 1, reader.err
}

func TestAdditionalDataEncodesEveryUnsignedFieldInOrder(t *testing.T) {
	t.Parallel()

	identity := bytes.Repeat([]byte{0x7f}, storeIdentityBytes)
	actual := additionalData(identity, 0x0102030405060708, 0x1112131415161718, 0x2122232425262728, 0x3132333435363738)
	want := make([]byte, len(aadVersion)+storeIdentityBytes+32)
	copy(want, aadVersion)
	copy(want[len(aadVersion):], identity)
	offset := len(aadVersion) + storeIdentityBytes
	binary.BigEndian.PutUint64(want[offset:], 0x0102030405060708)
	binary.BigEndian.PutUint64(want[offset+8:], 0x1112131415161718)
	binary.BigEndian.PutUint64(want[offset+16:], 0x2122232425262728)
	binary.BigEndian.PutUint64(want[offset+24:], 0x3132333435363738)
	if !bytes.Equal(actual, want) {
		t.Fatalf("additional data = %x, want %x", actual, want)
	}
}

func TestNonceDomainAllocatorIsProcessUniqueAndFailsClosed(t *testing.T) {
	t.Parallel()

	failing := nonceDomainAllocator{entropy: errorReader{err: io.ErrUnexpectedEOF}}
	if _, ok := failing.allocate(); ok {
		t.Fatal("allocator accepted failed process entropy")
	}
	allocator := nonceDomainAllocator{entropy: zeroReader{}}
	first, firstOK := allocator.allocate()
	second, secondOK := allocator.allocate()
	if !firstOK || !secondOK || first != 1 || second != 2 {
		t.Fatalf("allocated domains = (%d, %t), (%d, %t)", first, firstOK, second, secondOK)
	}
	allocator.next = ^uint64(0)
	if _, ok := allocator.allocate(); ok {
		t.Fatal("allocator reused a process nonce domain after exhaustion")
	}
}

func TestRecordBufferUsesStrictOrderingAndExactRemovalOffsets(t *testing.T) {
	t.Parallel()

	buffer := recordBuffer{data: []byte{1, 1, 2, 2, 1, 1}, recordBytes: 2}
	if !buffer.Less(0, 1) || buffer.Less(1, 0) || buffer.Less(0, 2) {
		t.Fatal("record buffer ordering is not strict lexicographic order")
	}

	buffer.removeOne([]byte{2, 2})
	if want := []byte{1, 1, 1, 1}; !bytes.Equal(buffer.data, want) {
		t.Fatalf("record buffer after middle removal = %v, want %v", buffer.data, want)
	}
	buffer.removeOne([]byte{0, 0})
	if want := []byte{1, 1, 1, 1}; !bytes.Equal(buffer.data, want) {
		t.Fatalf("record buffer changed after missing removal = %v", buffer.data)
	}
}

func TestRecordHeapUsesStrictRecordAndReaderOrdering(t *testing.T) {
	t.Parallel()

	records := recordHeap{
		{record: []byte{1}, reader: 1},
		{record: []byte{1}, reader: 2},
		{record: []byte{2}, reader: 0},
	}
	if !records.Less(0, 1) || records.Less(1, 0) || records.Less(0, 0) {
		t.Fatal("equal heap records do not use a strict reader tie-break")
	}
	if !records.Less(0, 2) || records.Less(2, 0) {
		t.Fatal("heap records do not use strict lexicographic order")
	}
}

type fixedAEAD struct {
	plaintext []byte
}

func (fixedAEAD) NonceSize() int {
	return 1
}

func (fixedAEAD) Overhead() int {
	return 1
}

func (fixedAEAD) Seal(destination []byte, _ []byte, plaintext []byte, _ []byte) []byte {
	return append(destination, plaintext...)
}

func (cipher fixedAEAD) Open(destination []byte, _ []byte, _ []byte, _ []byte) ([]byte, error) {
	return append(destination, cipher.plaintext...), nil
}
