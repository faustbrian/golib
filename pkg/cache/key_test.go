package cache_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestKeySpaceBuildsDeterministicCollisionSafeKeys(t *testing.T) {
	t.Parallel()

	space, err := cache.NewKeySpace("billing", "invoice", 3, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}

	first, err := space.Key("tenant:42/invoice:7")
	if err != nil {
		t.Fatal(err)
	}
	second, err := space.Key("tenant:42/invoice:7")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("key encoding is not deterministic: %q != %q", first, second)
	}
	if strings.Contains(first, "tenant:42") || strings.Contains(first, "invoice:7") {
		t.Fatalf("logical key leaked into backend key: %q", first)
	}
	if !strings.HasPrefix(first, "billing:invoice:v3:") {
		t.Fatalf("key lacks namespace and version: %q", first)
	}
}

func TestKeySpaceRejectsInvalidConfigurationAndOversizedKeys(t *testing.T) {
	t.Parallel()

	_, err := cache.NewKeySpace("bad:namespace", "invoice", 1, cache.StringKeyEncoder{}, 128)
	if !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("expected invalid namespace error, got %v", err)
	}

	space, err := cache.NewKeySpace("billing", "invoice", 1, cache.StringKeyEncoder{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	_, err = space.Key("invoice-7")
	if !errors.Is(err, cache.ErrKeyTooLarge) {
		t.Fatalf("expected key size error, got %v", err)
	}
}

func TestKeySpaceRejectsEveryInvalidComponent(t *testing.T) {
	t.Parallel()

	longPart := strings.Repeat("a", 65)
	tests := map[string]struct {
		namespace string
		name      string
		version   uint32
		encoder   cache.KeyEncoder[string]
		maxSize   int
	}{
		"empty namespace":     {"", "name", 1, cache.StringKeyEncoder{}, 128},
		"long namespace":      {longPart, "name", 1, cache.StringKeyEncoder{}, 128},
		"uppercase namespace": {"Bad", "name", 1, cache.StringKeyEncoder{}, 128},
		"invalid name":        {"namespace", "bad name", 1, cache.StringKeyEncoder{}, 128},
		"zero version":        {"namespace", "name", 0, cache.StringKeyEncoder{}, 128},
		"nil encoder":         {"namespace", "name", 1, nil, 128},
		"zero maximum":        {"namespace", "name", 1, cache.StringKeyEncoder{}, 0},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := cache.NewKeySpace(test.namespace, test.name, test.version, test.encoder, test.maxSize)
			if !errors.Is(err, cache.ErrInvalidKey) {
				t.Fatalf("NewKeySpace returned %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestKeySpaceClassifiesEncoderFailure(t *testing.T) {
	t.Parallel()

	cause := errors.New("canonicalization failed")
	space, err := cache.NewKeySpace("billing", "invoice", 1, failingKeyEncoder{err: cause}, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = space.Key("key")
	if !errors.Is(err, cache.ErrInvalidKey) || !strings.Contains(fmt.Sprint(err), cause.Error()) {
		t.Fatalf("Key returned %v", err)
	}
}

type failingKeyEncoder struct{ err error }

func (e failingKeyEncoder) EncodeKey(string) ([]byte, error) { return nil, e.err }
