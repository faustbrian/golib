package externalsort

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func BenchmarkEncryptedExternalSort(b *testing.B) {
	const records = 4096

	parent := b.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		b.Fatalf("Chmod() error = %v", err)
	}
	key := bytes.Repeat([]byte{1}, AES256KeyBytes)
	input := make([][]byte, records)
	for index := range input {
		record := make([]byte, 32)
		record[0] = byte(records - index)
		record[1] = byte((records - index) >> 8)
		input[index] = record
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		factory, err := NewFactory(Config{
			ParentDirectory: parent,
			RecordBytes:     32,
			ChunkRecords:    512,
			MaximumRecords:  records,
		})
		if err != nil {
			b.Fatalf("NewFactory() error = %v", err)
		}
		store, err := factory.Open(ctx, key)
		if err != nil {
			if store != nil {
				_ = store.Close()
			}
			b.Fatalf("Open() error = %v", err)
		}
		for _, record := range input {
			if err := store.Add(ctx, record); err != nil {
				b.Fatalf("Add() error = %v", err)
			}
		}
		if err := store.ForEachSorted(
			ctx,
			func([]byte) error { return nil },
		); err != nil {
			b.Fatalf("ForEachSorted() error = %v", err)
		}
		if err := store.Close(); err != nil {
			b.Fatalf("Close() error = %v", err)
		}
	}
}
