package externalsort

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"testing"
)

func FuzzRecordBufferSort(f *testing.F) {
	f.Add(byte(4), []byte{3, 3, 3, 3, 1, 1, 1, 1})
	f.Add(byte(1), []byte{2, 1, 2, 1})
	f.Add(byte(8), []byte{})

	f.Fuzz(func(t *testing.T, rawSize byte, input []byte) {
		recordBytes := int(rawSize%64) + 1
		completeBytes := len(input) - len(input)%recordBytes
		if completeBytes > 4096 {
			completeBytes = 4096 - 4096%recordBytes
		}
		buffer := recordBuffer{
			data:        append([]byte(nil), input[:completeBytes]...),
			recordBytes: recordBytes,
		}
		expected := make([][]byte, buffer.Len())
		for index := range expected {
			expected[index] = bytes.Clone(buffer.record(index))
		}
		sort.Slice(expected, func(left int, right int) bool {
			return bytes.Compare(expected[left], expected[right]) < 0
		})
		sort.Sort(&buffer)
		for index := range expected {
			if !bytes.Equal(buffer.record(index), expected[index]) {
				t.Fatalf(
					"record %d = %x, want %x",
					index,
					buffer.record(index),
					expected[index],
				)
			}
		}
	})
}

func FuzzConfigurationBounds(f *testing.F) {
	f.Add(uint32(1), uint32(1), uint32(1), true)
	f.Add(uint32(MaximumRecordBytes), uint32(256), uint32(256), true)
	f.Add(uint32(MaximumRecordBytes+1), uint32(1), uint32(1), true)
	f.Add(uint32(1), uint32(1), uint32(MaximumMergeFiles+1), true)

	f.Fuzz(func(
		t *testing.T,
		recordBytesRaw uint32,
		chunkRecordsRaw uint32,
		maximumRecordsRaw uint32,
		absoluteParent bool,
	) {
		recordBytes := int(recordBytesRaw)
		chunkRecords := int(chunkRecordsRaw)
		maximumRecords := int(maximumRecordsRaw)
		parent := "relative"
		if absoluteParent {
			parent = "/external-sort-fuzz"
		}
		want := absoluteParent &&
			recordBytes > 0 && recordBytes <= MaximumRecordBytes &&
			chunkRecords > 0 && chunkRecords <= MaximumChunkRecords &&
			maximumRecords >= chunkRecords &&
			recordBytes <= MaximumChunkBytes/chunkRecords
		if want {
			chunks := maximumRecords / chunkRecords
			if maximumRecords%chunkRecords != 0 {
				chunks++
			}
			want = chunks <= MaximumMergeFiles
		}
		actual := validConfig(Config{
			ParentDirectory: parent,
			RecordBytes:     recordBytes,
			ChunkRecords:    chunkRecords,
			MaximumRecords:  maximumRecords,
		})
		if actual != want {
			t.Fatalf("validConfig() = %t, want %t", actual, want)
		}
	})
}

func FuzzEncryptedChunkFraming(f *testing.F) {
	const recordBytes = 4

	block, err := aes.NewCipher(bytes.Repeat([]byte{1}, AES256KeyBytes))
	if err != nil {
		f.Fatalf("NewCipher() error = %v", err)
	}
	authenticatedCipher, err := cipher.NewGCM(block)
	if err != nil {
		f.Fatalf("NewGCM() error = %v", err)
	}
	nonce := bytes.Repeat([]byte{1}, authenticatedCipher.NonceSize())
	identity := bytes.Repeat([]byte{2}, storeIdentityBytes)
	plaintext := []byte{1, 2, 3, 4}
	valid := authenticatedCipher.Seal(
		bytes.Clone(nonce),
		nonce,
		plaintext,
		additionalData(identity, 0, 0, 0, recordBytes),
	)
	f.Add(valid)
	f.Add(valid[:len(valid)-1])
	f.Add(append(bytes.Clone(valid), 0))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			input = input[:4096]
		}
		var expected []byte
		if len(input) == authenticatedCipher.NonceSize()+recordBytes+authenticatedCipher.Overhead() {
			expected, _ = authenticatedCipher.Open(
				nil,
				input[:authenticatedCipher.NonceSize()],
				input[authenticatedCipher.NonceSize():],
				additionalData(identity, 0, 0, 0, recordBytes),
			)
		}
		reader := &chunkReader{
			file:            &fakeChunkFile{reader: bytes.NewReader(input)},
			cipher:          authenticatedCipher,
			identity:        identity,
			nonceDomain:     0,
			recordBytes:     recordBytes,
			expectedRecords: 1,
		}
		actual, readErr := reader.next()
		if expected != nil {
			if readErr != nil || !bytes.Equal(actual, expected) {
				t.Fatalf("next() = %x, %v, want %x, nil", actual, readErr, expected)
			}
			if _, readErr = reader.next(); !errors.Is(readErr, io.EOF) {
				t.Fatalf("terminal next() error = %v, want EOF", readErr)
			}

			return
		}
		if readErr == nil {
			_, readErr = reader.next()
		}
		if !errors.Is(readErr, ErrCorrupt) {
			t.Fatalf("invalid framing error = %v, want corrupt", readErr)
		}
	})
}

func FuzzMergeHistories(f *testing.F) {
	f.Add(byte(3), []byte{9, 1, 4, 1, 5, 9})
	f.Add(byte(8), []byte{})
	f.Add(byte(1), []byte{3, 2, 1})

	f.Fuzz(func(t *testing.T, rawChunks byte, rawInput []byte) {
		if len(rawInput) > 128 {
			rawInput = rawInput[:128]
		}
		chunkCount := int(rawChunks%8) + 1
		chunks := make([][]byte, chunkCount)
		for index, value := range rawInput {
			chunks[index%chunkCount] = append(chunks[index%chunkCount], value)
		}
		for index := range chunks {
			sort.Slice(chunks[index], func(left int, right int) bool {
				return chunks[index][left] < chunks[index][right]
			})
		}

		block, err := aes.NewCipher(bytes.Repeat([]byte{1}, AES256KeyBytes))
		if err != nil {
			t.Fatalf("NewCipher() error = %v", err)
		}
		authenticatedCipher, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatalf("NewGCM() error = %v", err)
		}
		identity := bytes.Repeat([]byte{2}, storeIdentityBytes)
		readers := make([]*chunkReader, len(chunks))
		for chunkIndex, records := range chunks {
			var framed bytes.Buffer
			for recordIndex, value := range records {
				nonce := make([]byte, authenticatedCipher.NonceSize())
				binary.BigEndian.PutUint32(nonce[4:8], uint32(chunkIndex))
				binary.BigEndian.PutUint32(nonce[8:], uint32(recordIndex))
				framed.Write(authenticatedCipher.Seal(
					bytes.Clone(nonce),
					nonce,
					[]byte{value},
					additionalData(
						identity,
						0,
						uint64(chunkIndex),
						uint64(recordIndex),
						1,
					),
				))
			}
			readers[chunkIndex] = &chunkReader{
				file:            &fakeChunkFile{reader: bytes.NewReader(framed.Bytes())},
				cipher:          authenticatedCipher,
				identity:        identity,
				nonceDomain:     0,
				recordBytes:     1,
				chunkIndex:      uint64(chunkIndex),
				expectedRecords: uint64(len(records)),
			}
		}

		output := make([]byte, 0, len(rawInput))
		if err := merge(
			context.Background(),
			readers,
			func(record []byte) error {
				output = append(output, record[0])

				return nil
			},
		); err != nil {
			t.Fatalf("merge() error = %v", err)
		}
		expected := bytes.Clone(rawInput)
		sort.Slice(expected, func(left int, right int) bool {
			return expected[left] < expected[right]
		})
		if !bytes.Equal(output, expected) {
			t.Fatalf("merge() = %x, want %x", output, expected)
		}
	})
}
