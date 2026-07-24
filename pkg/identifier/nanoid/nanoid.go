// Package nanoid implements compact random identifiers with configurable,
// bias-free ASCII alphabets and a mandatory 120-bit entropy floor.
package nanoid

import (
	"bytes"
	cryptorand "crypto/rand"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

const (
	// DefaultAlphabet is the URL-safe alphabet used by the reference NanoID.
	DefaultAlphabet = "_-0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// DefaultSize is the reference NanoID length.
	DefaultSize = 21
	// MinimumEntropyBits prevents configurations below the package's security
	// floor. NanoIDs remain identifiers, not secrets or authorization evidence.
	MinimumEntropyBits = 120
	maximumSize        = 1024
)

// Config defines one unambiguous byte alphabet and fixed identifier length.
type Config struct {
	Alphabet string
	Size     int
}

// DefaultConfig returns the 21-character URL-safe NanoID configuration.
func DefaultConfig() Config { return Config{Alphabet: DefaultAlphabet, Size: DefaultSize} }

// Validate rejects alphabets that could bias or ambiguously encode output and
// configurations below 120 bits of ideal entropy.
func (config Config) Validate() error {
	if len(config.Alphabet) < 2 {
		return fmt.Errorf("%w: NanoID alphabet must contain at least 2 bytes", identifier.ErrInvalid)
	}
	var seen [128]bool
	for index := range len(config.Alphabet) {
		character := config.Alphabet[index]
		if character < 0x21 || character > 0x7e {
			return fmt.Errorf("%w: NanoID alphabet must use printable ASCII", identifier.ErrInvalid)
		}
		if seen[character] {
			return fmt.Errorf("%w: NanoID alphabet contains duplicate %q", identifier.ErrInvalid, character)
		}
		seen[character] = true
	}
	if config.Size > maximumSize {
		return fmt.Errorf("%w: NanoID size must not exceed %d", identifier.ErrInvalid, maximumSize)
	}
	entropy := float64(config.Size) * math.Log2(float64(len(config.Alphabet)))
	if entropy < MinimumEntropyBits {
		return fmt.Errorf("%w: NanoID has %.1f bits; at least %d required", identifier.ErrInvalid, entropy, MinimumEntropyBits)
	}

	return nil
}

// ID is an immutable NanoID carrying the configuration required to validate
// subsequent decoding. Its zero value decodes with DefaultConfig.
type ID struct {
	text   string
	config Config
}

// Parse validates a default NanoID.
func Parse(text string) (ID, error) { return ParseWithConfig(text, DefaultConfig()) }

// ParseWithConfig validates text against an explicit alphabet and length.
func ParseWithConfig(text string, config Config) (ID, error) {
	if err := config.Validate(); err != nil {
		return ID{}, err
	}
	if len(text) != config.Size {
		return ID{}, fmt.Errorf("%w: NanoID length is %d, want %d", identifier.ErrInvalid, len(text), config.Size)
	}
	for index := range len(text) {
		if !strings.ContainsRune(config.Alphabet, rune(text[index])) {
			return ID{}, fmt.Errorf("%w: NanoID contains a byte outside its alphabet", identifier.ErrInvalid)
		}
	}

	return ID{text: text, config: config}, nil
}

// Prepare returns an unassigned decoding target for a custom configuration.
func Prepare(config Config) (ID, error) {
	if err := config.Validate(); err != nil {
		return ID{}, err
	}

	return ID{config: config}, nil
}

// Config returns the validation configuration carried by an ID.
func (id ID) Config() Config {
	if id.config.Alphabet == "" {
		return DefaultConfig()
	}

	return id.config
}

// String returns the NanoID text.
func (id ID) String() string { return id.text }

// LogValue redacts the NanoID from structured logs.
func (id ID) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// IsZero reports whether no NanoID has been assigned.
func (id ID) IsZero() bool { return id.text == "" }

// Compare returns lexical ordering. NanoID generation itself is not sortable.
func (id ID) Compare(other ID) int { return strings.Compare(id.text, other.text) }

// Inspect reports that NanoIDs define neither time nor sortable generation.
func (id ID) Inspect() identifier.Inspection {
	return identifier.Inspection{Family: identifier.FamilyNanoID}
}

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("%w: unassigned NanoID", identifier.ErrInvalid)
	}

	return []byte(id.text), nil
}

// UnmarshalText validates using the prepared configuration or DefaultConfig.
func (id *ID) UnmarshalText(text []byte) error {
	parsed, err := ParseWithConfig(string(text), id.Config())
	if err != nil {
		return err
	}
	*id = parsed

	return nil
}

// MarshalBinary returns NanoID text bytes.
func (id ID) MarshalBinary() ([]byte, error) { return id.MarshalText() }

// UnmarshalBinary validates NanoID text bytes.
func (id *ID) UnmarshalBinary(data []byte) error { return id.UnmarshalText(data) }

// MarshalJSON encodes a NanoID string or null when unassigned.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(id.text)
}

// UnmarshalJSON validates a string and accepts null as unassigned while
// retaining a prepared custom configuration.
func (id *ID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		id.text = ""

		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode NanoID: %w", err)
	}

	return id.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer using identifier text.
func (id ID) Value() (driver.Value, error) {
	if id.IsZero() {
		return nil, nil
	}

	return id.text, nil
}

// Scan implements sql.Scanner for text and NULL.
func (id *ID) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		id.text = ""

		return nil
	case string:
		return id.UnmarshalText([]byte(value))
	case []byte:
		return id.UnmarshalText(value)
	default:
		return fmt.Errorf("%w: cannot scan NanoID from %T", identifier.ErrInvalid, src)
	}
}

// Generator owns a validated configuration, entropy source, and reader lock.
type Generator struct {
	mutex   sync.Mutex
	config  Config
	entropy io.Reader
	mask    byte
	step    int
}

// NewGenerator constructs a bias-free NanoID generator. A nil reader selects
// crypto/rand.Reader for this generator instance.
func NewGenerator(config Config, entropy io.Reader) (*Generator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if entropy == nil {
		entropy = cryptorand.Reader
	}

	mask := byte(1)
	for int(mask) < len(config.Alphabet)-1 {
		mask = mask<<1 | 1
	}
	step := int(math.Ceil(1.6 * float64(int(mask)*config.Size) / float64(len(config.Alphabet))))

	return &Generator{config: config, entropy: entropy, mask: mask, step: step}, nil
}

// New uses rejection sampling so every alphabet position has equal probability.
func (generator *Generator) New() (ID, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()

	output := make([]byte, 0, generator.config.Size)
	buffer := make([]byte, generator.step)
	for range 128 {
		if _, err := io.ReadFull(generator.entropy, buffer); err != nil {
			return ID{}, fmt.Errorf("%w: NanoID: %w", identifier.ErrEntropy, err)
		}
		for _, random := range buffer {
			index := int(random & generator.mask)
			if index >= len(generator.config.Alphabet) {
				continue
			}
			output = append(output, generator.config.Alphabet[index])
			if len(output) == generator.config.Size {
				return ID{text: string(output), config: generator.config}, nil
			}
		}
	}

	return ID{}, fmt.Errorf("%w: NanoID rejection limit", identifier.ErrEntropy)
}
