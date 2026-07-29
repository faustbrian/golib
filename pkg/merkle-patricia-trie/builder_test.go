package mpt_test

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestSortedBuilderMatchesOrdinaryInsertion(t *testing.T) {
	t.Parallel()
	generator := rand.New(rand.NewPCG(0x6d7074, 0x6275696c64))
	entries := make([]struct{ key, value []byte }, 512)
	seen := make(map[string]struct{}, len(entries))
	for index := range entries {
		for {
			key := make([]byte, generator.IntN(16)+1)
			for offset := range key {
				key[offset] = byte(generator.Uint32())
			}
			if _, duplicate := seen[string(key)]; duplicate {
				continue
			}
			seen[string(key)] = struct{}{}
			value := make([]byte, generator.IntN(96)+1)
			for offset := range value {
				value[offset] = byte(generator.Uint32())
			}
			entries[index] = struct{ key, value []byte }{key: key, value: value}
			break
		}
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].key, entries[right].key) < 0
	})

	builder, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for _, entry := range entries {
		if err := builder.Add(context.Background(), entry.key, entry.value); err != nil {
			t.Fatalf("Add(%x) error = %v", entry.key, err)
		}
		trie, err = trie.Update(context.Background(), entry.key, entry.value)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", entry.key, err)
		}
	}
	got, err := builder.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	want, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if got != want {
		t.Fatalf("Finalize() = %x, want %x", got, want)
	}
}

func TestSortedBuilderOrderingAndLifecycle(t *testing.T) {
	t.Parallel()
	builder, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte("b"), []byte("one")); err != nil {
		t.Fatalf("Add(b) error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte("b"), []byte("two")); !errors.Is(err, mpt.ErrDuplicateBuilderKey) {
		t.Fatalf("duplicate Add() error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte("a"), []byte("two")); !errors.Is(err, mpt.ErrOutOfOrderKey) {
		t.Fatalf("out-of-order Add() error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte("c"), []byte("three")); err != nil {
		t.Fatalf("Add(c) after rejected inputs error = %v", err)
	}
	if _, err := builder.Finalize(context.Background()); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte("d"), []byte("four")); !errors.Is(err, mpt.ErrClosedBuilder) {
		t.Fatalf("Add() after Finalize error = %v", err)
	}
	if _, err := builder.Finalize(context.Background()); !errors.Is(err, mpt.ErrClosedBuilder) {
		t.Fatalf("second Finalize() error = %v", err)
	}
}

func TestSortedBuilderEmptyAndEmptyKey(t *testing.T) {
	t.Parallel()
	empty, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	root, err := empty.Finalize(context.Background())
	if err != nil {
		t.Fatalf("empty Finalize() error = %v", err)
	}
	if root != mpt.EmptyRoot() {
		t.Fatalf("empty Finalize() = %x, want %x", root, mpt.EmptyRoot())
	}

	builder, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	value := []byte("empty-key")
	if err := builder.Add(context.Background(), nil, value); err != nil {
		t.Fatalf("Add(empty) error = %v", err)
	}
	value[0] = 'X'
	got, err := builder.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), nil, []byte("empty-key"))
	if err != nil {
		t.Fatalf("Update(empty) error = %v", err)
	}
	want, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if got != want {
		t.Fatalf("Finalize() = %x, want %x", got, want)
	}
}

func TestSortedBuilderRejectsInvalidInputsWithoutMutation(t *testing.T) {
	t.Parallel()
	limits := mpt.DefaultLimits()
	limits.MaxKeyBytes = 2
	limits.MaxValueBytes = 2
	builder, err := mpt.NewSortedBuilder(limits)
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	var nilContext context.Context
	if err := builder.Add(
		nilContext, []byte{1}, []byte{1},
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := builder.Add(canceled, []byte{1}, []byte{1}); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte{1, 2, 3}, []byte{1}); !errors.Is(err, mpt.ErrInvalidKey) {
		t.Fatalf("oversized key error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte{1}, nil); !errors.Is(err, mpt.ErrInvalidValue) {
		t.Fatalf("empty value error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte{1}, []byte{1, 2, 3}); !errors.Is(err, mpt.ErrInvalidValue) {
		t.Fatalf("oversized value error = %v", err)
	}
	if err := builder.Add(context.Background(), []byte{1}, []byte{2}); err != nil {
		t.Fatalf("valid Add() error = %v", err)
	}
	root, err := builder.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	trie, _ := mpt.NewRawTrie(limits)
	trie, _ = trie.Update(context.Background(), []byte{1}, []byte{2})
	want, _ := trie.Root()
	if !slices.Equal(root.Bytes(), want.Bytes()) {
		t.Fatalf("Finalize() = %x, want %x", root, want)
	}
}

func TestSortedBuilderZeroValue(t *testing.T) {
	t.Parallel()
	var builder mpt.SortedBuilder
	if err := builder.Add(context.Background(), []byte{1}, []byte{1}); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero Add() error = %v", err)
	}
	if _, err := builder.Finalize(context.Background()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero Finalize() error = %v", err)
	}
}
