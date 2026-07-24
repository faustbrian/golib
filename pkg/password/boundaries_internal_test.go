package password

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func boundaryLimits() Limits {
	return Limits{PasswordBytes: 1, EncodedHashBytes: 256, Argon2Time: 4, MemoryKiB: 64, Parallelism: 4, SaltBytes: 16, OutputBytes: 32, BcryptCost: 14, Concurrent: 1, Queue: 0}
}

func boundaryArgonConfig() PolicyConfig {
	return PolicyConfig{Algorithm: Argon2id, Argon2id: Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: boundaryLimits()}
}

func TestPolicyExactBoundaries(t *testing.T) {
	valid := boundaryArgonConfig()
	if _, err := NewPolicy(valid); err != nil {
		t.Fatalf("minimum policy: %v", err)
	}
	exact := PolicyConfig{
		Algorithm: Argon2id,
		Argon2id:  Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16},
		Limits:    Limits{PasswordBytes: 1, Argon2Time: 1, MemoryKiB: 8, Parallelism: 1, SaltBytes: 8, OutputBytes: 16, BcryptCost: 4, Concurrent: 1, Queue: 0},
	}
	exact.Limits.EncodedHashBytes = int(argon2idEncodedLength(exact.Argon2id)) //nolint:gosec // Fixed minimum parameters encode far below MaxInt.
	if _, err := NewPolicy(exact); err != nil {
		t.Fatalf("exact minimum limits: %v", err)
	}
	upper := valid
	upper.Argon2id = Argon2idParameters{Version: 19, Time: upper.Limits.Argon2Time, MemoryKiB: upper.Limits.MemoryKiB, Parallelism: upper.Limits.Parallelism, SaltLength: upper.Limits.SaltBytes, OutputLength: upper.Limits.OutputBytes}
	if _, err := NewPolicy(upper); err != nil {
		t.Fatalf("maximum policy: %v", err)
	}

	mutations := []func(*PolicyConfig){
		func(c *PolicyConfig) { c.Limits.PasswordBytes = 0 },
		func(c *PolicyConfig) { c.Limits.EncodedHashBytes = 59 },
		func(c *PolicyConfig) { c.Limits.Concurrent = 0 },
		func(c *PolicyConfig) { c.Limits.Queue = -1 },
		func(c *PolicyConfig) { c.Argon2id.Time = 0 },
		func(c *PolicyConfig) { c.Argon2id.Time = c.Limits.Argon2Time + 1 },
		func(c *PolicyConfig) { c.Argon2id.MemoryKiB = 7 },
		func(c *PolicyConfig) { c.Argon2id.MemoryKiB = c.Limits.MemoryKiB + 1 },
		func(c *PolicyConfig) { c.Argon2id.Parallelism = 0 },
		func(c *PolicyConfig) { c.Argon2id.Parallelism = c.Limits.Parallelism + 1 },
		func(c *PolicyConfig) { c.Argon2id.SaltLength = 7 },
		func(c *PolicyConfig) { c.Argon2id.SaltLength = c.Limits.SaltBytes + 1 },
		func(c *PolicyConfig) { c.Argon2id.OutputLength = 15 },
		func(c *PolicyConfig) { c.Argon2id.OutputLength = c.Limits.OutputBytes + 1 },
	}
	for index, mutate := range mutations {
		config := valid
		mutate(&config)
		if _, err := NewPolicy(config); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("mutation %d error = %v", index, err)
		}
	}

	limits := boundaryLimits()
	for _, cost := range []int{4, limits.BcryptCost} {
		if _, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: cost, Limits: limits}); err != nil {
			t.Fatalf("valid bcrypt cost %d: %v", cost, err)
		}
	}
	limits.EncodedHashBytes = 60
	if _, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: 4, Limits: limits}); err != nil {
		t.Fatalf("bcrypt exact encoding limit: %v", err)
	}
	for _, cost := range []int{3, limits.BcryptCost + 1} {
		if _, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: cost, Limits: limits}); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("invalid bcrypt cost %d: %v", cost, err)
		}
	}
	limits.BcryptCost = 32
	if _, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: 4, Limits: limits}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("bcrypt limit 32: %v", err)
	}
	limits.BcryptCost = 31
	if _, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: 31, Limits: limits}); err != nil {
		t.Fatalf("bcrypt limit 31: %v", err)
	}
}

func TestRawBase64EncodedLengthMatchesStandardLibrary(t *testing.T) {
	for length := range 129 {
		got := rawBase64EncodedLength(uint32(length))
		want := uint64(base64.RawStdEncoding.EncodedLen(length)) //nolint:gosec // EncodedLen is non-negative for the bounded input.
		if got != want {
			t.Fatalf("length %d = %d, want %d", length, got, want)
		}
	}
}

func TestParserExactBoundaries(t *testing.T) {
	limits := boundaryLimits()
	encode := func(memory, timeCost, parallelism int, salt, output []byte) string {
		return "$argon2id$v=19$m=" + itoa(memory) + ",t=" + itoa(timeCost) + ",p=" + itoa(parallelism) + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(output)
	}
	salt := make([]byte, 8)
	output := make([]byte, 16)
	for _, encoded := range []string{
		encode(8, 1, 1, salt, output),
		encode(10, 1, 1, salt, output),
		encode(int(limits.MemoryKiB), int(limits.Argon2Time), int(limits.Parallelism), make([]byte, limits.SaltBytes), make([]byte, limits.OutputBytes)),
	} {
		if _, err := ParseEncodedHash(encoded, limits); err != nil {
			t.Fatalf("valid boundary hash: %v", err)
		}
	}
	bad := []string{
		encode(7, 1, 1, salt, output), encode(65, 1, 1, salt, output),
		encode(8, 0, 1, salt, output), encode(8, 5, 1, salt, output),
		encode(8, 1, 0, salt, output), encode(40, 1, 5, salt, output),
		encode(8, 1, 1, make([]byte, 7), output), encode(8, 1, 1, make([]byte, 17), output),
		encode(8, 1, 1, salt, make([]byte, 15)), encode(8, 1, 1, salt, make([]byte, 33)),
		strings.Replace(encode(8, 1, 1, salt, output), "m=8", "m=08", 1),
	}
	for index, encoded := range bad {
		if _, err := ParseEncodedHash(encoded, limits); err == nil {
			t.Fatalf("invalid boundary hash %d accepted", index)
		}
	}
	zeroParallelism := encode(8, 1, 0, salt, output)
	if _, err := ParseEncodedHash(zeroParallelism, limits); !errors.Is(err, ErrResourceRejected) {
		t.Fatalf("zero parallelism classification: %v", err)
	}

	encoded := encode(8, 1, 1, salt, output)
	limits.EncodedHashBytes = len(encoded)
	if _, err := ParseEncodedHash(encoded, limits); err != nil {
		t.Fatalf("exact encoded limit: %v", err)
	}
	limits.EncodedHashBytes--
	if _, err := ParseEncodedHash(encoded, limits); !errors.Is(err, ErrResourceRejected) {
		t.Fatalf("over encoded limit: %v", err)
	}

	bcrypt := "$2b$04$" + strings.Repeat(".", 53)
	limits = boundaryLimits()
	if _, err := ParseEncodedHash(bcrypt, limits); err != nil {
		t.Fatalf("minimum bcrypt cost: %v", err)
	}
	bcrypt = "$2b$14$" + strings.Repeat(".", 53)
	if _, err := ParseEncodedHash(bcrypt, limits); err != nil {
		t.Fatalf("maximum bcrypt cost: %v", err)
	}
}

func itoa(value int) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return itoa(value/10) + string(digits[value%10])
}
