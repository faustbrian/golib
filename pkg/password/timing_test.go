package password_test

import (
	"context"
	"slices"
	"testing"
	"time"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestArgon2idMatchMismatchTimingSmoke(t *testing.T) {
	svc, err := password.NewTestService(testArgonPolicy(t), &repeatingReader{value: 7})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := svc.Hash(context.Background(), []byte("synthetic timing password"))
	if err != nil {
		t.Fatal(err)
	}
	assertMatchMismatchTimingSmoke(t, svc, hash.String(), []byte("synthetic timing password"))
}

func TestBcryptMatchMismatchTimingSmoke(t *testing.T) {
	svc, err := password.New(testBcryptPolicy(t, 6))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := svc.Hash(context.Background(), []byte("synthetic timing password"))
	if err != nil {
		t.Fatal(err)
	}
	assertMatchMismatchTimingSmoke(t, svc, hash.String(), []byte("synthetic timing password"))
}

func assertMatchMismatchTimingSmoke(t *testing.T, svc *password.Service, encoded string, matching []byte) {
	t.Helper()
	measure := func(secret []byte, durations []time.Duration, index int) {
		start := time.Now()
		_, _ = svc.Verify(context.Background(), secret, encoded)
		durations[index] = time.Since(start)
	}
	const samples = 201
	matches := make([]time.Duration, samples)
	mismatches := make([]time.Duration, samples)
	for index := range samples {
		if index%2 == 0 {
			measure(matching, matches, index)
			measure([]byte("different synthetic value"), mismatches, index)
		} else {
			measure([]byte("different synthetic value"), mismatches, index)
			measure(matching, matches, index)
		}
	}
	slices.Sort(matches)
	slices.Sort(mismatches)
	for _, percentile := range []int{10, 50, 90} {
		index := (samples - 1) * percentile / 100
		match := matches[index]
		mismatch := mismatches[index]
		if match > mismatch*5 || mismatch > match*5 {
			t.Fatalf("obvious timing regression at p%d: match=%s mismatch=%s", percentile, match, mismatch)
		}
	}
}

func TestMalformedHashTimingStaysBeforePrimitive(t *testing.T) {
	limits := testLimits()
	limits.MemoryKiB = 1024
	policy, err := password.NewPolicy(password.PolicyConfig{
		Algorithm: password.Argon2id,
		Argon2id:  password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 1024, Parallelism: 1, SaltLength: 8, OutputLength: 16},
		Limits:    limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := password.NewTestService(policy, &repeatingReader{value: 7})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := svc.Hash(context.Background(), []byte("synthetic timing password"))
	if err != nil {
		t.Fatal(err)
	}
	const samples = 51
	malformed := make([]time.Duration, samples)
	mismatches := make([]time.Duration, samples)
	for index := range samples {
		start := time.Now()
		_, _ = svc.Verify(context.Background(), []byte("different synthetic value"), "malformed")
		malformed[index] = time.Since(start)
		start = time.Now()
		_, _ = svc.Verify(context.Background(), []byte("different synthetic value"), hash.String())
		mismatches[index] = time.Since(start)
	}
	slices.Sort(malformed)
	slices.Sort(mismatches)
	if malformed[45] >= mismatches[5] {
		t.Fatalf("malformed path reached primitive timing: malformed_p90=%s mismatch_p10=%s", malformed[45], mismatches[5])
	}
}

type repeatingReader struct{ value byte }

func (r *repeatingReader) Read(destination []byte) (int, error) {
	for index := range destination {
		destination[index] = r.value
	}
	return len(destination), nil
}
