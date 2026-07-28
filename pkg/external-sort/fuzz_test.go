package externalsort

import (
	"bytes"
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
