// Package httpsignature implements HTTP Message Signatures and digest fields.
package httpsignature

import (
	"cmp"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"

	"github.com/dunglas/httpsfv"
)

// DigestAlgorithm is an RFC 9530 Hash Algorithms for HTTP Digest Fields key.
type DigestAlgorithm string

const (
	// SHA256 identifies the active sha-256 digest algorithm.
	SHA256 DigestAlgorithm = "sha-256"
	// SHA512 identifies the active sha-512 digest algorithm.
	SHA512 DigestAlgorithm = "sha-512"
)

var (
	// ErrInvalidDigestField reports malformed, empty, or ambiguous digest data.
	ErrInvalidDigestField = errors.New("http signature: invalid digest field")
	// ErrUnsupportedDigestAlgorithm reports an algorithm unavailable for local computation.
	ErrUnsupportedDigestAlgorithm = errors.New("http signature: unsupported digest algorithm")
	// ErrMissingDigest reports that policy selected an algorithm absent from the field.
	ErrMissingDigest = errors.New("http signature: required digest is missing")
	// ErrDigestMismatch reports that content does not match a selected digest.
	ErrDigestMismatch = errors.New("http signature: digest mismatch")
)

// Digest is one algorithm and checksum member of an RFC 9530 integrity field.
type Digest struct {
	Algorithm  DigestAlgorithm
	Value      []byte
	Parameters []Parameter
}

// DigestField is an ordered, immutable RFC 9530 integrity-field dictionary.
// Parsed fields preserve wire order; locally computed fields use algorithm-key
// order so output does not depend on caller or map iteration order.
type DigestField struct {
	entries []Digest
}

// ComputeDigests calculates an integrity field over content using active
// algorithms from the RFC 9530 registry. At least one unique algorithm is
// required.
func ComputeDigests(algorithms []DigestAlgorithm, content []byte) (DigestField, error) {
	if len(algorithms) == 0 {
		return DigestField{}, ErrInvalidDigestField
	}

	seen := make(map[DigestAlgorithm]struct{}, len(algorithms))
	entries := make([]Digest, 0, len(algorithms))

	for _, algorithm := range algorithms {
		if _, exists := seen[algorithm]; exists {
			return DigestField{}, fmt.Errorf("%w: duplicate algorithm", ErrInvalidDigestField)
		}

		value, err := checksum(algorithm, content)
		if err != nil {
			return DigestField{}, err
		}

		seen[algorithm] = struct{}{}
		entries = append(entries, Digest{Algorithm: algorithm, Value: value})
	}

	slices.SortFunc(entries, func(left, right Digest) int {
		return cmp.Compare(left.Algorithm, right.Algorithm)
	})

	return DigestField{entries: entries}, nil
}

// ParseDigestField parses the RFC 9530 Dictionary form used by Content-Digest
// and Repr-Digest. It rejects duplicate keys and values other than Structured
// Fields Byte Sequences.
func ParseDigestField(value string) (DigestField, error) {
	return ParseDigestFields([]string{value})
}

// ParseDigestFields combines and parses all field lines for one Content-Digest
// or Repr-Digest field. Duplicate algorithm keys are rejected across lines.
func ParseDigestFields(values []string) (DigestField, error) {
	return ParseDigestFieldsWithLimits(values, DefaultSyntaxLimits())
}

// ParseDigestFieldsWithLimits combines and parses integrity field lines under
// explicit resource bounds.
func ParseDigestFieldsWithLimits(values []string, limits SyntaxLimits) (DigestField, error) {
	if err := enforceRawSyntaxLimits(values, limits); err != nil {
		if errors.Is(err, ErrInvalidSyntaxLimits) {
			return DigestField{}, err
		}
		return DigestField{}, fmt.Errorf("%w: %w", ErrInvalidDigestField, ErrSyntaxLimit)
	}
	if err := rejectDuplicateDictionaryKeys(values); err != nil {
		return DigestField{}, fmt.Errorf("%w: %v", ErrInvalidDigestField, err)
	}
	dictionary, err := unmarshalStructuredDictionary(normalizeStructuredFieldOWS(values))
	if err != nil || len(dictionary.Names()) == 0 {
		return DigestField{}, ErrInvalidDigestField
	}
	if len(dictionary.Names()) > limits.MaxDictionaryMembers {
		return DigestField{}, fmt.Errorf("%w: %w", ErrInvalidDigestField, ErrSyntaxLimit)
	}
	entries := make([]Digest, 0, len(dictionary.Names()))
	for _, name := range dictionary.Names() {
		member, _ := dictionary.Get(name)
		item, ok := member.(httpsfv.Item)
		if !ok {
			return DigestField{}, ErrInvalidDigestField
		}
		value, ok := item.Value.([]byte)
		if !ok {
			return DigestField{}, ErrInvalidDigestField
		}
		parameters, parameterErr := copyParameters(item.Params)
		if parameterErr != nil {
			return DigestField{}, ErrInvalidDigestField
		}
		if len(value) > limits.MaxBinaryBytes || len(parameters) > limits.MaxParametersPerItem {
			return DigestField{}, fmt.Errorf("%w: %w", ErrInvalidDigestField, ErrSyntaxLimit)
		}
		entries = append(entries, Digest{
			Algorithm: DigestAlgorithm(name), Value: append([]byte(nil), value...), Parameters: parameters,
		})
	}

	return DigestField{entries: entries}, nil
}

// Entries returns a deep copy of the ordered digest members.
func (field DigestField) Entries() []Digest {
	entries := make([]Digest, len(field.entries))
	for index, entry := range field.entries {
		entries[index] = Digest{
			Algorithm:  entry.Algorithm,
			Value:      append([]byte(nil), entry.Value...),
			Parameters: cloneParameters(entry.Parameters),
		}
	}

	return entries
}

// String serializes the digest dictionary using canonical Byte Sequence
// encoding and the field's stable member order.
func (field DigestField) String() string {
	dictionary := httpsfv.NewDictionary()
	for _, entry := range field.entries {
		item := httpsfv.NewItem(append([]byte(nil), entry.Value...))
		addParameters(item.Params, entry.Parameters)
		dictionary.Add(string(entry.Algorithm), item)
	}
	value, err := httpsfv.Marshal(dictionary)
	if err != nil {
		panic(fmt.Errorf("serialize validated digest field: %w", err))
	}
	return value
}

// Verify computes and constant-time compares every policy-selected digest.
// Unknown, unselected field members are retained but ignored as RFC 9530
// permits recipients to ignore any digest.
func (field DigestField) Verify(content []byte, required []DigestAlgorithm) error {
	if len(required) == 0 {
		return ErrInvalidDigestField
	}

	entries := make(map[DigestAlgorithm][]byte, len(field.entries))
	for _, entry := range field.entries {
		entries[entry.Algorithm] = entry.Value
	}

	seen := make(map[DigestAlgorithm]struct{}, len(required))
	for _, algorithm := range required {
		if _, duplicate := seen[algorithm]; duplicate {
			return fmt.Errorf("%w: duplicate required algorithm", ErrInvalidDigestField)
		}
		seen[algorithm] = struct{}{}

		actual, exists := entries[algorithm]
		if !exists {
			return fmt.Errorf("%w: %s", ErrMissingDigest, algorithm)
		}

		expected, err := checksum(algorithm, content)
		if err != nil {
			return err
		}

		if len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
			return fmt.Errorf("%w: %s", ErrDigestMismatch, algorithm)
		}
	}

	return nil
}

func checksum(algorithm DigestAlgorithm, content []byte) ([]byte, error) {
	switch algorithm {
	case SHA256:
		sum := sha256.Sum256(content)

		return sum[:], nil
	case SHA512:
		sum := sha512.Sum512(content)

		return sum[:], nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDigestAlgorithm, algorithm)
	}
}

func isKeyStart(character byte) bool {
	return character == '*' || character >= 'a' && character <= 'z'
}

func isKeyCharacter(character byte) bool {
	return isKeyStart(character) || character >= '0' && character <= '9' ||
		character == '_' || character == '-' || character == '.'
}
