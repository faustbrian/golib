package identifier_test

import (
	"sort"
	"sync"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	"github.com/faustbrian/golib/pkg/identifier/idtest"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

const concurrentGenerationCount = 4096

func TestEverySharedGeneratorHasAtomicUniqueStateTransitions(t *testing.T) {
	instant := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	t.Run("UUIDv4", func(t *testing.T) {
		assertConcurrentUnique(t,
			identifieruuid.NewV4Generator(idtest.NewReader([]byte("concurrent-uuid-v4"))),
			concurrentGenerationCount,
		)
	})
	t.Run("UUIDv7", func(t *testing.T) {
		values := assertConcurrentUnique(t, identifieruuid.NewV7Generator(
			idtest.NewClock(instant), idtest.NewReader([]byte("concurrent-uuid-v7")),
		), concurrentGenerationCount)
		assertSortedSequence(t, values, identifieruuid.ID.Compare)
	})
	t.Run("ULID", func(t *testing.T) {
		values := assertConcurrentUnique(t, identifierulid.NewGenerator(
			idtest.NewClock(instant), idtest.NewReader([]byte("concurrent-ulid")),
		), concurrentGenerationCount)
		assertSortedSequence(t, values, identifierulid.ID.Compare)
	})
	t.Run("TypeID", func(t *testing.T) {
		generator, err := identifiertypeid.NewGenerator("worker", identifieruuid.NewV7Generator(
			idtest.NewClock(instant), idtest.NewReader([]byte("concurrent-typeid")),
		))
		if err != nil {
			t.Fatal(err)
		}
		values := assertConcurrentUnique(t, generator, concurrentGenerationCount)
		assertSortedSequence(t, values, identifiertypeid.ID.Compare)
	})
	t.Run("KSUID", func(t *testing.T) {
		values := assertConcurrentUnique(t, identifierksuid.NewGenerator(
			idtest.NewClock(instant), idtest.NewReader([]byte("concurrent-ksuid")),
		), concurrentGenerationCount)
		assertSortedSequence(t, values, identifierksuid.ID.Compare)
	})
	t.Run("NanoID", func(t *testing.T) {
		generator, err := identifiernanoid.NewGenerator(
			identifiernanoid.DefaultConfig(), idtest.NewReader([]byte("concurrent-nanoid")),
		)
		if err != nil {
			t.Fatal(err)
		}
		assertConcurrentUnique(t, generator, concurrentGenerationCount)
	})
}

func assertConcurrentUnique[T comparable](
	t *testing.T,
	generator identifier.Generator[T],
	count int,
) []T {
	t.Helper()

	const workers = 32
	start := make(chan struct{})
	results := make(chan T, count)
	errors := make(chan error, count)
	var wait sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range count / workers {
				value, err := generator.New()
				if err != nil {
					errors <- err
					continue
				}
				results <- value
			}
		}()
	}

	close(start)
	wait.Wait()
	close(results)
	close(errors)

	for err := range errors {
		t.Fatalf("concurrent generation: %v", err)
	}

	seen := make(map[T]struct{}, count)
	values := make([]T, 0, count)
	for value := range results {
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate concurrent state transition: %v", value)
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) != count {
		t.Fatalf("generated %d identifiers, want %d", len(values), count)
	}

	return values
}

func assertSortedSequence[T any](t *testing.T, values []T, compare func(T, T) int) {
	t.Helper()

	sort.Slice(values, func(left, right int) bool {
		return compare(values[left], values[right]) < 0
	})
	idtest.AssertStrictlyOrdered(t, values, compare)
}
