package httpsignature

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDigestDictionaryKeyCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, character := range []byte{'*', 'a', 'z'} {
		if !isKeyStart(character) {
			t.Fatalf("isKeyStart(%q) = false", character)
		}
	}
	for _, character := range []byte{0, '`', '{', '0', '9', '_', '-', '.'} {
		if isKeyStart(character) {
			t.Fatalf("isKeyStart(%q) = true", character)
		}
	}
	for _, character := range []byte{'*', 'a', 'z', '0', '9', '_', '-', '.'} {
		if !isKeyCharacter(character) {
			t.Fatalf("isKeyCharacter(%q) = false", character)
		}
	}
	for _, character := range []byte{0, '`', '{', '/', ':'} {
		if isKeyCharacter(character) {
			t.Fatalf("isKeyCharacter(%q) = true", character)
		}
	}
}

func TestDigestParserAndVerifierBoundarySemantics(t *testing.T) {
	t.Parallel()

	limits := DefaultSyntaxLimits()
	invalidLimits := limits
	invalidLimits.MaxFieldBytes = 0
	if _, err := ParseDigestFieldsWithLimits([]string{"sha-256=:AA==:"}, invalidLimits); !errors.Is(err, ErrInvalidSyntaxLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	small := limits
	small.MaxFieldBytes = 2
	small.MaxBinaryBytes = 1
	if _, err := ParseDigestFieldsWithLimits([]string{"sha-256=:AA==:"}, small); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("field limit error = %v", err)
	}
	if _, err := ParseDigestFieldsWithLimits(nil, limits); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("empty field error = %v", err)
	}
	for _, value := range []string{
		"sha-256=(1 2)",
		"sha-256=1",
		"sha-256=:AA==:;date=@1",
	} {
		if _, err := ParseDigestField(value); !errors.Is(err, ErrInvalidDigestField) {
			t.Fatalf("ParseDigestField(%q) error = %v", value, err)
		}
	}
	members := limits
	members.MaxDictionaryMembers = 1
	if _, err := ParseDigestFieldsWithLimits([]string{"sha-256=:AA==:, sha-512=:AA==:"}, members); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("member limit error = %v", err)
	}
	binary := limits
	binary.MaxBinaryBytes = 1
	if _, err := ParseDigestFieldsWithLimits([]string{"sha-256=:AAE=:"}, binary); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("binary limit error = %v", err)
	}
	parameters := limits
	parameters.MaxParametersPerItem = 1
	if _, err := ParseDigestFieldsWithLimits([]string{"sha-256=:AA==:;x;y"}, parameters); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("parameter limit error = %v", err)
	}

	field, err := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if err := field.Verify([]byte("payload"), nil); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("empty policy error = %v", err)
	}
	if err := field.Verify([]byte("payload"), []DigestAlgorithm{SHA256, SHA256}); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("duplicate policy error = %v", err)
	}
	if err := field.Verify([]byte("payload"), []DigestAlgorithm{SHA512}); !errors.Is(err, ErrMissingDigest) {
		t.Fatalf("missing policy error = %v", err)
	}
	unsupported := DigestField{entries: []Digest{{Algorithm: "unsupported", Value: []byte("x")}}}
	if err := unsupported.Verify([]byte("payload"), []DigestAlgorithm{"unsupported"}); !errors.Is(err, ErrUnsupportedDigestAlgorithm) {
		t.Fatalf("unsupported policy error = %v", err)
	}
	short := DigestField{entries: []Digest{{Algorithm: SHA256, Value: []byte("x")}}}
	if err := short.Verify([]byte("payload"), []DigestAlgorithm{SHA256}); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("short digest error = %v", err)
	}
}

func TestDigestStringPanicsForImpossibleInvalidInternalKey(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("String() did not reject invalid internal algorithm key")
		}
	}()
	_ = (DigestField{entries: []Digest{{Algorithm: "UPPER", Value: []byte("x")}}}).String()
}

func TestDigestPreferenceBoundarySemantics(t *testing.T) {
	t.Parallel()

	for _, entries := range [][]DigestPreference{
		nil,
		{{Algorithm: "UPPER", Weight: 1}},
		{{Algorithm: SHA256, Weight: -1}},
		{{Algorithm: SHA256, Weight: 11}},
		{{Algorithm: SHA256, Weight: 1}, {Algorithm: SHA256, Weight: 2}},
	} {
		if _, err := NewDigestPreferences(entries); !errors.Is(err, ErrInvalidDigestPreferences) {
			t.Fatalf("NewDigestPreferences(%#v) error = %v", entries, err)
		}
	}
	limits := DefaultSyntaxLimits()
	invalid := limits
	invalid.MaxFieldBytes = 0
	if _, err := ParseDigestPreferencesWithLimits([]string{"sha-256=1"}, invalid); !errors.Is(err, ErrInvalidSyntaxLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	small := limits
	small.MaxFieldBytes = 2
	small.MaxBinaryBytes = 1
	if _, err := ParseDigestPreferencesWithLimits([]string{"sha-256=1"}, small); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("field limit error = %v", err)
	}
	if _, err := ParseDigestPreferencesWithLimits(nil, limits); !errors.Is(err, ErrInvalidDigestPreferences) {
		t.Fatalf("empty field error = %v", err)
	}
	members := limits
	members.MaxDictionaryMembers = 1
	if _, err := ParseDigestPreferencesWithLimits([]string{"sha-256=1, sha-512=1"}, members); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("member limit error = %v", err)
	}
	for _, value := range []string{"Bad", "sha-256=9999999999999999", `sha-256="unterminated`, "sha-256=(1)", "sha-256=1;x", "sha-256=?1"} {
		if _, err := ParseDigestPreferences([]string{value}); !errors.Is(err, ErrInvalidDigestPreferences) {
			t.Fatalf("ParseDigestPreferences(%q) error = %v", value, err)
		}
	}
	for _, algorithm := range []DigestAlgorithm{"", "UPPER", "sha 256"} {
		if validDigestAlgorithmKey(algorithm) {
			t.Fatalf("validDigestAlgorithmKey(%q) = true", algorithm)
		}
	}
}

func TestDigestPreferenceStringPanicsForImpossibleInvalidInternalKey(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("String() did not reject invalid internal algorithm key")
		}
	}()
	_ = (DigestPreferences{entries: []DigestPreference{{Algorithm: "UPPER", Weight: 1}}}).String()
}

func TestMemoryReplayStoreCancellationBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Second)}
	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if err := store.Consume(nil, record); err == nil || !strings.Contains(err.Error(), "nil context") { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Consume(ctx, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-lock cancellation error = %v", err)
	}
	var nilStore *MemoryReplayStore
	if err := nilStore.Consume(context.Background(), record); !errors.Is(err, ErrInvalidReplayConfig) {
		t.Fatalf("nil store error = %v", err)
	}

	store.mu.Lock()
	blockedCtx, blockedCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- store.Consume(blockedCtx, record) }()
	time.Sleep(10 * time.Millisecond)
	blockedCancel()
	store.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("post-lock cancellation error = %v", err)
	}
}
