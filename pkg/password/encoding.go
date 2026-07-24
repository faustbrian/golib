package password

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var bcryptBase64 = base64.NewEncoding("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789").WithPadding(base64.NoPadding)

// EncodedHash is a validated persistence value. String explicitly returns the
// encoded hash; all fmt-based diagnostic formatting is redacted.
type EncodedHash struct {
	encoded    string
	algorithm  Algorithm
	argon2id   Argon2idParameters
	bcryptCost int
	salt       []byte
	digest     []byte
}

// String returns the persistence encoding and must not be used for diagnostics.
func (h EncodedHash) String() string { return h.encoded }

// GoString returns a redacted Go-syntax diagnostic representation.
func (h EncodedHash) GoString() string { return "password.EncodedHash{redacted}" }

// Format redacts every fmt formatting verb, including %s, %v, and %#v.
func (h EncodedHash) Format(s fmt.State, _ rune) { _, _ = fmt.Fprint(s, "[password hash redacted]") }

// Algorithm returns the parsed algorithm.
func (h EncodedHash) Algorithm() Algorithm { return h.algorithm }

// Argon2idParameters returns parsed parameters or zero values for bcrypt.
func (h EncodedHash) Argon2idParameters() Argon2idParameters { return h.argon2id }

// BcryptCost returns the parsed cost or zero for Argon2id.
func (h EncodedHash) BcryptCost() int { return h.bcryptCost }

// ParseEncodedHash validates canonical syntax and resource fields before any
// password primitive is invoked.
func ParseEncodedHash(encoded string, limits Limits) (EncodedHash, error) {
	if len(encoded) == 0 {
		return EncodedHash{}, newError(ErrMalformedHash, "parse hash", nil)
	}
	if len(encoded) > limits.EncodedHashBytes {
		return EncodedHash{}, newError(ErrResourceRejected, "parse hash", nil)
	}
	if strings.HasPrefix(encoded, "$argon2id$") {
		return parseArgon2id(encoded, limits)
	}
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") || strings.HasPrefix(encoded, "$2y$") {
		return parseBcrypt(encoded, limits)
	}
	if strings.HasPrefix(encoded, "$") {
		return EncodedHash{}, newError(ErrUnsupportedAlgorithm, "parse hash", nil)
	}
	return EncodedHash{}, newError(ErrMalformedHash, "parse hash", nil)
}

func parseArgon2id(encoded string, limits Limits) (EncodedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return EncodedHash{}, newError(ErrMalformedHash, "parse argon2id", nil)
	}
	if parts[2] != "v=19" {
		if strings.HasPrefix(parts[2], "v=") {
			return EncodedHash{}, newError(ErrUnsupportedVersion, "parse argon2id", nil)
		}
		return EncodedHash{}, newError(ErrMalformedHash, "parse argon2id", nil)
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return EncodedHash{}, newError(ErrMalformedHash, "parse argon2id", nil)
	}
	read := func(field, prefix string) (uint64, error) {
		if !strings.HasPrefix(field, prefix) || len(field) == len(prefix) {
			return 0, ErrMalformedHash
		}
		number := field[len(prefix):]
		if len(number) > 1 && number[0] == '0' {
			return 0, ErrMalformedHash
		}
		for _, char := range number {
			if char < '0' || char > '9' {
				return 0, ErrMalformedHash
			}
		}
		value, err := strconv.ParseUint(number, 10, 32)
		if err != nil {
			return 0, ErrResourceRejected
		}
		return value, nil
	}
	m, e1 := read(params[0], "m=")
	t, e2 := read(params[1], "t=")
	p, e3 := read(params[2], "p=")
	if e1 != nil || e2 != nil || e3 != nil {
		if errors.Is(e1, ErrResourceRejected) || errors.Is(e2, ErrResourceRejected) || errors.Is(e3, ErrResourceRejected) {
			return EncodedHash{}, newError(ErrResourceRejected, "parse argon2id", nil)
		}
		return EncodedHash{}, newError(ErrMalformedHash, "parse argon2id", nil)
	}
	if m < 8*p || m > uint64(limits.MemoryKiB) || p < 1 || p > uint64(limits.Parallelism) || t < 1 || t > uint64(limits.Argon2Time) {
		return EncodedHash{}, newError(ErrResourceRejected, "parse argon2id", nil)
	}
	if uint64(len(parts[4])) > rawBase64EncodedLength(limits.SaltBytes) || uint64(len(parts[5])) > rawBase64EncodedLength(limits.OutputBytes) {
		return EncodedHash{}, newError(ErrResourceRejected, "parse argon2id", nil)
	}
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	out, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if e1 != nil || e2 != nil || len(salt) < 8 || len(out) < 16 {
		return EncodedHash{}, newError(ErrMalformedHash, "parse argon2id", nil)
	}
	memory := uint32(m)              //nolint:gosec // ParseUint above is explicitly limited to 32 bits.
	timeCost := uint32(t)            //nolint:gosec // ParseUint above is explicitly limited to 32 bits.
	parallelism := uint8(p)          //nolint:gosec // The resource check bounds p by the uint8 policy limit.
	saltLength := uint32(len(salt))  //nolint:gosec // Decoded length is bounded by the uint32 policy limit.
	outputLength := uint32(len(out)) //nolint:gosec // Decoded length is bounded by the uint32 policy limit.
	return EncodedHash{encoded: encoded, algorithm: Argon2id, argon2id: Argon2idParameters{Version: 19, Time: timeCost, MemoryKiB: memory, Parallelism: parallelism, SaltLength: saltLength, OutputLength: outputLength}, salt: salt, digest: out}, nil
}

func parseBcrypt(encoded string, limits Limits) (EncodedHash, error) {
	if len(encoded) != 60 || encoded[4] < '0' || encoded[4] > '9' || encoded[5] < '0' || encoded[5] > '9' || encoded[6] != '$' {
		return EncodedHash{}, newError(ErrMalformedHash, "parse bcrypt", nil)
	}
	cost, _ := strconv.Atoi(encoded[4:6])
	if cost < 4 || cost > limits.BcryptCost {
		return EncodedHash{}, newError(ErrResourceRejected, "parse bcrypt", nil)
	}
	for _, char := range encoded[7:] {
		if !strings.ContainsRune("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", char) {
			return EncodedHash{}, newError(ErrMalformedHash, "parse bcrypt", nil)
		}
	}
	salt, saltErr := bcryptBase64.Strict().DecodeString(encoded[7:29])
	digest, digestErr := bcryptBase64.Strict().DecodeString(encoded[29:])
	if saltErr != nil || digestErr != nil || len(salt) != 16 || len(digest) != 23 {
		return EncodedHash{}, newError(ErrMalformedHash, "parse bcrypt", nil)
	}
	return EncodedHash{encoded: encoded, algorithm: Bcrypt, bcryptCost: cost}, nil
}
