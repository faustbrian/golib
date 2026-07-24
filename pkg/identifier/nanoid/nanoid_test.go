package nanoid_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type recordingReader struct {
	reads int
	sizes []int
	fill  byte
}

func (reader *recordingReader) Read(output []byte) (int, error) {
	reader.reads++
	reader.sizes = append(reader.sizes, len(output))
	for index := range output {
		output[index] = reader.fill
	}

	return len(output), nil
}

func TestDefaultGeneratorUsesUnbiasedOwnedEntropy(t *testing.T) {
	generator, err := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	id, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := id.String(), "_____________________"; got != want {
		t.Fatalf("New() = %q, want %q", got, want)
	}
	inspection := id.Inspect()
	if inspection.Family != identifier.FamilyNanoID || inspection.HasTime || inspection.Sortable {
		t.Fatalf("Inspect() = %+v", inspection)
	}

	failing, err := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), failingReader{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("entropy error = %v", err)
	}
}

func TestConfigurationRejectsBiasAmbiguityAndLowEntropy(t *testing.T) {
	invalid := []identifiernanoid.Config{
		{Alphabet: "a", Size: 120},
		{Alphabet: "aabc", Size: 60},
		{Alphabet: "abé", Size: 120},
		{Alphabet: "ab", Size: 119},
		{Alphabet: "ab", Size: 0},
		{Alphabet: "ab", Size: 1025},
	}
	for _, config := range invalid {
		if err := config.Validate(); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("Validate(%+v) error = %v", config, err)
		}
		if _, err := identifiernanoid.NewGenerator(config, nil); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("NewGenerator(%+v) error = %v", config, err)
		}
	}
}

func TestConfigurationAndRejectionSamplingBoundaries(t *testing.T) {
	for _, config := range []identifiernanoid.Config{
		{Alphabet: "!~", Size: 120},
		{Alphabet: "ab", Size: 1024},
	} {
		if err := config.Validate(); err != nil {
			t.Fatalf("boundary config %+v: %v", config, err)
		}
	}

	requested := &recordingReader{}
	generator, err := identifiernanoid.NewGenerator(
		identifiernanoid.Config{Alphabet: "abc", Size: 76}, requested,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, generationErr := generator.New(); generationErr != nil {
		t.Fatal(generationErr)
	}
	if len(requested.sizes) != 1 || requested.sizes[0] != 122 {
		t.Fatalf("rejection buffer requests = %v", requested.sizes)
	}

	rejected := &recordingReader{fill: 3}
	generator, err = identifiernanoid.NewGenerator(
		identifiernanoid.Config{Alphabet: "abc", Size: 76}, rejected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("rejection limit error = %v", err)
	}
	if rejected.reads != 128 {
		t.Fatalf("rejection reads = %d", rejected.reads)
	}
}

func TestCustomAlphabetRejectsOutOfAlphabetAndWrongLength(t *testing.T) {
	config := identifiernanoid.Config{Alphabet: "ab", Size: 120}
	id, err := identifiernanoid.ParseWithConfig(string(bytes.Repeat([]byte{'a'}, 120)), config)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != string(bytes.Repeat([]byte{'a'}, 120)) {
		t.Fatalf("ParseWithConfig() = %q", id)
	}
	for _, input := range []string{
		string(bytes.Repeat([]byte{'a'}, 119)),
		string(bytes.Repeat([]byte{'c'}, 120)),
	} {
		if _, err := identifiernanoid.ParseWithConfig(input, config); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("ParseWithConfig() error = %v", err)
		}
	}
}

func TestSerializationAndPreparedCustomDecoding(t *testing.T) {
	original, _ := identifiernanoid.Parse("_____________________")
	if original.LogValue().String() != "[REDACTED]" {
		t.Fatal("NanoID log value was not redacted")
	}
	text, _ := original.MarshalText()
	var decoded identifiernanoid.ID
	if err := decoded.UnmarshalText(text); err != nil || decoded != original {
		t.Fatalf("text round trip = %s, %v", decoded, err)
	}
	binary, _ := original.MarshalBinary()
	if err := decoded.UnmarshalBinary(binary); err != nil || decoded != original {
		t.Fatalf("binary round trip = %s, %v", decoded, err)
	}
	data, _ := json.Marshal(original)
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != original {
		t.Fatalf("JSON round trip = %s, %v", decoded, err)
	}
	value, err := original.Value()
	if err != nil || value != original.String() {
		t.Fatalf("Value() = %v, %v", value, err)
	}
	for _, source := range []any{original.String(), []byte(original.String())} {
		if scanErr := decoded.Scan(source); scanErr != nil || decoded != original {
			t.Fatalf("Scan(%T) = %s, %v", source, decoded, scanErr)
		}
	}

	config := identifiernanoid.Config{Alphabet: "ab", Size: 120}
	prepared, err := identifiernanoid.Prepare(config)
	if err != nil {
		t.Fatal(err)
	}
	custom := string(bytes.Repeat([]byte{'b'}, 120))
	if err := prepared.UnmarshalText([]byte(custom)); err != nil || prepared.String() != custom {
		t.Fatalf("prepared custom decode = %q, %v", prepared, err)
	}
}

func TestDecodersRejectInvalidValuesAndHandleNull(t *testing.T) {
	var id identifiernanoid.ID
	for name, decode := range map[string]func() error{
		"text": func() error { return id.UnmarshalText([]byte("bad")) },
		"json": func() error { return json.Unmarshal([]byte("42"), &id) },
		"scan": func() error { return id.Scan(42) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := decode(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := id.Scan(nil); err != nil || !id.IsZero() {
		t.Fatalf("Scan(nil) = %s, %v", id, err)
	}
	value, err := id.Value()
	if err != nil || value != nil {
		t.Fatalf("zero Value() = %v, %v", value, err)
	}
}
