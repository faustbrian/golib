package externalsort

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSortsEncryptedFixedRecordsWithinDeclaredBounds(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory, err := NewFactory(Config{
		ParentDirectory: parent,
		RecordBytes:     32,
		ChunkRecords:    2,
		MaximumRecords:  5,
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	store, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{0x31}, AES256KeyBytes),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	workDirectory := onlyChildDirectory(t, parent)

	input := [][]byte{
		bytes.Repeat([]byte{5}, 32),
		bytes.Repeat([]byte{1}, 32),
		bytes.Repeat([]byte{3}, 32),
		bytes.Repeat([]byte{3}, 32),
		bytes.Repeat([]byte{2}, 32),
	}
	for _, record := range input {
		if err := store.Add(context.Background(), record); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	var output [][]byte
	if err := store.ForEachSorted(
		context.Background(),
		func(record []byte) error {
			output = append(output, append([]byte(nil), record...))

			return nil
		},
	); err != nil {
		t.Fatalf("ForEachSorted() error = %v", err)
	}
	want := [][]byte{
		bytes.Repeat([]byte{1}, 32),
		bytes.Repeat([]byte{2}, 32),
		bytes.Repeat([]byte{3}, 32),
		bytes.Repeat([]byte{3}, 32),
		bytes.Repeat([]byte{5}, 32),
	}
	if !equalRecords(output, want) {
		t.Fatalf("sorted records = %v, want %v", output, want)
	}

	entries, err := os.ReadDir(workDirectory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(entries))
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatalf("Info() error = %v", infoErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("chunk mode = %04o, want 0600", info.Mode().Perm())
		}
		contents, readErr := os.ReadFile(
			filepath.Join(workDirectory, entry.Name()),
		)
		if readErr != nil {
			t.Fatalf("ReadFile() error = %v", readErr)
		}
		for _, plaintext := range input {
			if bytes.Contains(contents, plaintext) {
				t.Fatal("temporary chunk contains a plaintext record")
			}
		}
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(workDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary directory remains after Close: %v", err)
	}
}

func ownerOnlyTemporaryDirectory(t *testing.T) string {
	t.Helper()

	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	return parent
}

func onlyChildDirectory(t *testing.T, parent string) string {
	t.Helper()

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("temporary entries = %#v", entries)
	}
	path := filepath.Join(parent, entries[0].Name())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("temporary directory mode = %04o, want 0700", info.Mode().Perm())
	}

	return path
}

func equalRecords(left [][]byte, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}

	return true
}
