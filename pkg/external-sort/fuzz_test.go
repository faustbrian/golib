package externalsort

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
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
		sort.Sort(&buffer)
		for index := 1; index < buffer.Len(); index++ {
			if bytes.Compare(
				buffer.record(index-1),
				buffer.record(index),
			) > 0 {
				t.Fatalf("records %d and %d are not sorted", index-1, index)
			}
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
	plaintext := []byte{1, 2, 3, 4}
	valid := authenticatedCipher.Seal(
		bytes.Clone(nonce),
		nonce,
		plaintext,
		additionalData(0, 0, recordBytes),
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
				additionalData(0, 0, recordBytes),
			)
		}
		reader := &chunkReader{
			file:            &fakeChunkFile{reader: bytes.NewReader(input)},
			cipher:          authenticatedCipher,
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
