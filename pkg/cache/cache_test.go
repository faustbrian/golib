package cache_test

import (
	"errors"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestResultStatesAreUnambiguous(t *testing.T) {
	t.Parallel()

	if cache.Hit == cache.Miss || cache.Hit == cache.Stale || cache.Miss == cache.Stale {
		t.Fatal("hit, miss, and stale states must be distinct")
	}

	result := cache.Result[string]{State: cache.Hit, Value: ""}
	if result.State != cache.Hit || result.Value != "" {
		t.Fatalf("stored zero value must remain a hit: %#v", result)
	}
}

func TestSentinelErrorsRemainClassifiableThroughOperationError(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	err := &cache.Error{Kind: cache.BackendError, Operation: cache.OperationGet, Cause: cause}

	if !errors.Is(err, cache.ErrBackend) {
		t.Fatalf("backend error must match ErrBackend: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("backend error must preserve its cause: %v", err)
	}
	if errors.Is(err, cache.ErrMiss) {
		t.Fatalf("backend error must not match ErrMiss: %v", err)
	}
}

func TestTTLPolicyValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		policy cache.TTLPolicy
		valid  bool
	}{
		"absolute":       {policy: cache.TTLPolicy{TTL: time.Minute}, valid: true},
		"sliding":        {policy: cache.TTLPolicy{TTL: time.Minute, Sliding: true}, valid: true},
		"stale":          {policy: cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Minute}, valid: true},
		"no ttl":         {policy: cache.TTLPolicy{}, valid: false},
		"negative ttl":   {policy: cache.TTLPolicy{TTL: -time.Second}, valid: false},
		"negative stale": {policy: cache.TTLPolicy{TTL: time.Minute, StaleFor: -time.Second}, valid: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := test.policy.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid policy: %v", err)
			}
			if !test.valid && !errors.Is(err, cache.ErrInvalidTTL) {
				t.Fatalf("expected ErrInvalidTTL, got %v", err)
			}
		})
	}
}

func TestNewClassifiesInvalidConfigurationAndLoadPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	valid := cache.Config[string, string]{
		Backend:  newRecordingBackend(),
		Keys:     mustStringKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: now},
		MaxValue: 1024,
	}

	configurationTests := map[string]func(*cache.Config[string, string]){
		"nil backend":        func(config *cache.Config[string, string]) { config.Backend = nil },
		"nil codec":          func(config *cache.Config[string, string]) { config.Codec = nil },
		"nil clock":          func(config *cache.Config[string, string]) { config.Clock = nil },
		"zero value limit":   func(config *cache.Config[string, string]) { config.MaxValue = 0 },
		"negative batch max": func(config *cache.Config[string, string]) { config.MaxBatch = -1 },
	}
	for name, mutate := range configurationTests {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			_, err := cache.New(config)
			if !errors.Is(err, cache.ErrInvalidConfig) {
				t.Fatalf("New returned %v, want ErrInvalidConfig", err)
			}
		})
	}

	policyTests := map[string]func(*cache.Config[string, string]){
		"negative cache TTL": func(config *cache.Config[string, string]) { config.Load.NegativeTTL = -time.Second },
		"negative loaders":   func(config *cache.Config[string, string]) { config.Load.MaxConcurrent = -1 },
		"negative waiters":   func(config *cache.Config[string, string]) { config.Load.MaxWaitersPerKey = -1 },
		"negative jitter":    func(config *cache.Config[string, string]) { config.Load.RefreshJitter = -time.Second },
	}
	for name, mutate := range policyTests {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			_, err := cache.New(config)
			if !errors.Is(err, cache.ErrInvalidPolicy) {
				t.Fatalf("New returned %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestOperationErrorsClassifyWithoutNilUnwrapChildren(t *testing.T) {
	t.Parallel()

	tests := map[cache.ErrorKind]error{
		cache.BackendError:        cache.ErrBackend,
		cache.DecodeError:         cache.ErrDecode,
		cache.SchemaMismatchError: cache.ErrSchemaMismatch,
		cache.InvalidKeyError:     cache.ErrInvalidKey,
		cache.LimitError:          cache.ErrValueTooLarge,
		cache.PolicyError:         cache.ErrInvalidPolicy,
		cache.LoaderError:         cache.ErrLoader,
	}
	for kind, sentinel := range tests {
		err := &cache.Error{Kind: kind, Operation: cache.OperationGet}
		if !errors.Is(err, sentinel) {
			t.Fatalf("kind %d does not match %v: %v", kind, sentinel, err)
		}
		for _, child := range err.Unwrap() {
			if child == nil {
				t.Fatalf("kind %d returned nil unwrap child", kind)
			}
		}
	}
	unknown := &cache.Error{Kind: cache.ErrorKind(255), Operation: cache.OperationGet}
	if children := unknown.Unwrap(); len(children) != 0 {
		t.Fatalf("unknown cause-free kind returned children: %#v", children)
	}
}

func TestOperationErrorStringsDescribeNilAndWrappedCauses(t *testing.T) {
	t.Parallel()

	var nilError *cache.Error
	if nilError.Error() != "<nil>" {
		t.Fatalf("nil error string: %q", nilError.Error())
	}
	withoutCause := &cache.Error{Operation: cache.OperationDelete}
	if withoutCause.Error() != "cache delete failed" {
		t.Fatalf("cause-free error string: %q", withoutCause.Error())
	}
	withCause := &cache.Error{Operation: cache.OperationGet, Cause: errors.New("offline")}
	if withCause.Error() != "cache get failed: offline" {
		t.Fatalf("wrapped error string: %q", withCause.Error())
	}
	if children := nilError.Unwrap(); children != nil {
		t.Fatalf("nil error unwrap returned %#v", children)
	}
	cause := errors.New("unknown failure")
	unknown := &cache.Error{Kind: cache.ErrorKind(255), Operation: cache.OperationGet, Cause: cause}
	children := unknown.Unwrap()
	if len(children) != 1 || !errors.Is(children[0], cause) {
		t.Fatalf("unknown kind did not preserve cause: %#v", children)
	}
}
