package ecmascript_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestHostileExecutionPathsAreBounded(t *testing.T) {
	t.Run("catastrophic backtracking", func(t *testing.T) {
		program := mustCompile(t, "(a|aa)*b", "")
		options := hostileMatchOptions()
		options.Limits.Steps = 1_000
		_, _, err := program.Find(
			context.Background(),
			strings.Repeat("a", 512),
			options,
		)
		assertLimit(t, err, ecmascript.LimitMatchSteps)
	})

	t.Run("zero width loop", func(t *testing.T) {
		program := mustCompile(t, "(?=a)*", "")
		result, matched, err := program.Find(
			context.Background(),
			"a",
			hostileMatchOptions(),
		)
		if err != nil || !matched || len(result.Full().Value().Units()) != 0 {
			t.Fatalf("Find() = _, %t, %v; want bounded empty match", matched, err)
		}
	})

	t.Run("huge capture graph", func(t *testing.T) {
		options := ecmascript.DefaultCompileOptions()
		options.Parse.Limits.Captures = 32
		_, err := ecmascript.Compile(strings.Repeat("(a)", 33), "", options)
		assertLimit(t, err, ecmascript.LimitCaptures)
	})

	t.Run("nested assertions", func(t *testing.T) {
		pattern := strings.Repeat("(?=", 8) + "a" + strings.Repeat(")", 8)
		program := mustCompile(t, pattern, "")
		options := hostileMatchOptions()
		options.Limits.RecursionDepth = 4
		_, _, err := program.Find(context.Background(), "a", options)
		assertLimit(t, err, ecmascript.LimitRecursionDepth)
	})

	t.Run("replacement expansion", func(t *testing.T) {
		program := mustCompile(t, "a", "g")
		options := hostileMatchOptions()
		options.Limits.OutputUTF16 = 64
		_, err := program.Replace(
			context.Background(),
			strings.Repeat("a", 128),
			ecmascript.UTF16FromString("$&$&"),
			options,
		)
		assertLimit(t, err, ecmascript.LimitOutputUTF16)
	})

	t.Run("Unicode sets", func(t *testing.T) {
		program := mustCompile(t, "[[a-z]&&[^aeiou]]+", "v")
		options := hostileMatchOptions()
		options.Limits.Steps = 32
		_, _, err := program.Find(
			context.Background(),
			strings.Repeat("b", 128),
			options,
		)
		assertLimit(t, err, ecmascript.LimitMatchSteps)
	})

	t.Run("malformed UTF-8", func(t *testing.T) {
		program := mustCompile(t, ".", "u")
		result, matched, err := program.Find(
			context.Background(),
			string([]byte{0xff, 0xfe}),
			hostileMatchOptions(),
		)
		if err != nil || !matched {
			t.Fatalf("Find() = _, %t, %v; want replacement-character match", matched, err)
		}
		span := result.Full().Span()
		if span.Start.Byte != 0 || span.End.Byte != 1 ||
			result.Full().Value().LossyString() != "\uFFFD" {
			t.Fatalf("malformed UTF-8 match = %#v, %q", span, result.Full().Value().LossyString())
		}
	})
}

func TestImmutableProgramsAndCallerOwnedCachesAreRaceSafe(t *testing.T) {
	t.Parallel()

	type programCache struct {
		sync.RWMutex
		programs map[string]*ecmascript.Program
	}
	program := mustCompile(t, "(?<word>\\p{Letter}+)", "u")
	cache := &programCache{programs: map[string]*ecmascript.Program{"word": program}}
	errorsFound := make(chan error, 32)
	var workers sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for iteration := 0; iteration < 100; iteration++ {
				cache.RLock()
				cached := cache.programs["word"]
				cache.RUnlock()
				result, matched, err := cached.Find(
					context.Background(),
					"42 Helsinki",
					ecmascript.DefaultMatchOptions(),
				)
				if err != nil || !matched || result.Full().Value().LossyString() != "Helsinki" {
					errorsFound <- errors.New("concurrent cached match diverged")
					return
				}
				cache.Lock()
				cache.programs["word"] = cached
				cache.Unlock()
			}
		}()
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
}

func TestExecutionDoesNotLeakGoroutinesOrBuffers(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	program := mustCompile(t, "(a|aa)*b", "")
	input := strings.Repeat("a", 64<<10)
	options := hostileMatchOptions()
	options.Limits.Steps = 100

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for iteration := 0; iteration < 64; iteration++ {
		_, _, err := program.Find(context.Background(), input, options)
		assertLimit(t, err, ecmascript.LimitMatchSteps)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	const retainedLimit = 8 << 20
	if after.HeapAlloc > before.HeapAlloc+retainedLimit {
		t.Fatalf(
			"repeated bounded execution retained %d bytes; limit %d",
			after.HeapAlloc-before.HeapAlloc,
			retainedLimit,
		)
	}
}

func hostileMatchOptions() ecmascript.MatchOptions {
	options := ecmascript.DefaultMatchOptions()
	options.Limits.InputBytes = 1 << 20
	options.Limits.InputRunes = 1 << 20
	options.Limits.Steps = 100_000
	options.Limits.Backtracks = 10_000
	options.Limits.StackDepth = 1_000
	options.Limits.RecursionDepth = 32
	options.Limits.Allocations = 100_000
	options.Limits.Results = 1_000
	options.Limits.OutputUTF16 = 1 << 20
	return options
}

func mustCompile(t *testing.T, pattern, flags string) *ecmascript.Program {
	t.Helper()
	program, err := ecmascript.Compile(
		pattern,
		flags,
		ecmascript.DefaultCompileOptions(),
	)
	if err != nil {
		t.Fatalf("Compile(%q, %q) error = %v", pattern, flags, err)
	}
	return program
}

func assertLimit(t *testing.T, err error, kind ecmascript.LimitKind) {
	t.Helper()
	var limitError *ecmascript.LimitError
	if !errors.As(err, &limitError) || limitError.Kind != kind {
		t.Fatalf("error = %v; want limit kind %d", err, kind)
	}
}
