package httpsignature

import (
	"errors"
	"fmt"

	"github.com/dunglas/httpsfv"
)

// ErrInvalidDigestPreferences reports a malformed RFC 9530 integrity
// preference dictionary.
var ErrInvalidDigestPreferences = errors.New("http signature: invalid digest preferences")

// DigestPreference is one algorithm and relative weight from a
// Want-Content-Digest or Want-Repr-Digest field. Weight is in the inclusive
// range 0 through 10; zero means not acceptable.
type DigestPreference struct {
	Algorithm DigestAlgorithm
	Weight    int64
}

// DigestPreferences is an immutable ordered RFC 9530 integrity preference
// dictionary. Order is preserved because equal weights have no protocol-level
// tie-breaking semantics.
type DigestPreferences struct {
	entries []DigestPreference
}

// NewDigestPreferences validates and copies an ordered preference dictionary.
func NewDigestPreferences(entries []DigestPreference) (DigestPreferences, error) {
	if len(entries) == 0 {
		return DigestPreferences{}, ErrInvalidDigestPreferences
	}

	result := make([]DigestPreference, len(entries))
	seen := make(map[DigestAlgorithm]struct{}, len(entries))
	for index, entry := range entries {
		if !validDigestAlgorithmKey(entry.Algorithm) || entry.Weight < 0 || entry.Weight > 10 {
			return DigestPreferences{}, ErrInvalidDigestPreferences
		}
		if _, duplicate := seen[entry.Algorithm]; duplicate {
			return DigestPreferences{}, fmt.Errorf("%w: duplicate algorithm", ErrInvalidDigestPreferences)
		}
		seen[entry.Algorithm] = struct{}{}
		result[index] = entry
	}

	return DigestPreferences{entries: result}, nil
}

// ParseDigestPreferences parses combined Want-Content-Digest or
// Want-Repr-Digest field lines. Unknown algorithm keys are retained for
// application policy and interoperability.
func ParseDigestPreferences(values []string) (DigestPreferences, error) {
	return ParseDigestPreferencesWithLimits(values, DefaultSyntaxLimits())
}

// ParseDigestPreferencesWithLimits parses integrity preferences under explicit
// resource bounds.
func ParseDigestPreferencesWithLimits(values []string, limits SyntaxLimits) (DigestPreferences, error) {
	if err := enforceRawSyntaxLimits(values, limits); err != nil {
		if errors.Is(err, ErrInvalidSyntaxLimits) {
			return DigestPreferences{}, err
		}
		return DigestPreferences{}, fmt.Errorf("%w: %w", ErrInvalidDigestPreferences, ErrSyntaxLimit)
	}
	if err := rejectDuplicateDictionaryKeys(values); err != nil {
		return DigestPreferences{}, fmt.Errorf("%w: %v", ErrInvalidDigestPreferences, err)
	}

	dictionary, err := unmarshalStructuredDictionary(normalizeStructuredFieldOWS(values))
	if err != nil || len(dictionary.Names()) == 0 {
		return DigestPreferences{}, fmt.Errorf("%w: malformed structured field", ErrInvalidDigestPreferences)
	}
	if len(dictionary.Names()) > limits.MaxDictionaryMembers {
		return DigestPreferences{}, fmt.Errorf("%w: %w", ErrInvalidDigestPreferences, ErrSyntaxLimit)
	}

	entries := make([]DigestPreference, 0, len(dictionary.Names()))
	for _, name := range dictionary.Names() {
		member, _ := dictionary.Get(name)
		item, ok := member.(httpsfv.Item)
		if !ok {
			return DigestPreferences{}, ErrInvalidDigestPreferences
		}
		if len(item.Params.Names()) != 0 {
			return DigestPreferences{}, ErrInvalidDigestPreferences
		}
		weight, ok := item.Value.(int64)
		if !ok || weight < 0 || weight > 10 {
			return DigestPreferences{}, ErrInvalidDigestPreferences
		}
		entries = append(entries, DigestPreference{Algorithm: DigestAlgorithm(name), Weight: weight})
	}

	return NewDigestPreferences(entries)
}

// Entries returns a copy in combined-field wire order.
func (preferences DigestPreferences) Entries() []DigestPreference {
	return append([]DigestPreference(nil), preferences.entries...)
}

// String returns the canonical Structured Fields serialization.
func (preferences DigestPreferences) String() string {
	dictionary := httpsfv.NewDictionary()
	for _, entry := range preferences.entries {
		dictionary.Add(string(entry.Algorithm), httpsfv.NewItem(entry.Weight))
	}
	value, err := marshalRFC8941(dictionary)
	if err != nil {
		panic(fmt.Errorf("serialize validated digest preferences: %w", err))
	}
	return value
}

func validDigestAlgorithmKey(algorithm DigestAlgorithm) bool {
	value := string(algorithm)
	if value == "" || !isKeyStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !isKeyCharacter(value[index]) {
			return false
		}
	}
	return true
}
