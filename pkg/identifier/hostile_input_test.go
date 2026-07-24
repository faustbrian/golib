package identifier_test

import (
	"errors"
	"strings"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
	segmentksuid "github.com/segmentio/ksuid"
)

func TestUUIDParserExhaustsSingleByteMutations(t *testing.T) {
	const canonical = "017f22e2-79b0-7cc3-98c4-dc0c0c07398f"
	for length := 0; length <= len(canonical)*2; length++ {
		if length == len(canonical) {
			continue
		}
		if _, err := identifieruuid.Parse(strings.Repeat("0", length)); !errors.Is(err, identifier.ErrInvalid) {
			t.Fatalf("length %d error = %v", length, err)
		}
	}

	for position := range len(canonical) {
		for candidate := 0; candidate <= 255; candidate++ {
			mutated := []byte(canonical)
			mutated[position] = byte(candidate)
			_, err := identifieruuid.Parse(string(mutated))
			wantValid := validUUIDByte(position, byte(candidate))
			if (err == nil) != wantValid {
				t.Fatalf("position %d byte 0x%02x valid=%v error=%v", position, candidate, wantValid, err)
			}
		}
	}
}

func validUUIDByte(position int, candidate byte) bool {
	switch position {
	case 8, 13, 18, 23:
		return candidate == '-'
	case 14:
		return candidate >= '1' && candidate <= '8'
	case 19:
		return candidate == '8' || candidate == '9' || candidate == 'a' || candidate == 'b'
	default:
		return candidate >= '0' && candidate <= '9' || candidate >= 'a' && candidate <= 'f'
	}
}

func TestULIDParserExhaustsSingleByteMutations(t *testing.T) {
	const canonical = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for position := range len(canonical) {
		for candidate := 0; candidate <= 255; candidate++ {
			mutated := []byte(canonical)
			mutated[position] = byte(candidate)
			_, err := identifierulid.Parse(string(mutated))
			wantValid := strings.ContainsRune(alphabet, rune(candidate)) &&
				(position != 0 || candidate <= '7')
			if (err == nil) != wantValid {
				t.Fatalf("position %d byte 0x%02x valid=%v error=%v", position, candidate, wantValid, err)
			}
		}
	}
}

func TestTypeIDParserRunsCompleteOfficialVectorCorpus(t *testing.T) {
	valid := []string{
		"00000000000000000000000000",
		"00000000000000000000000001",
		"0000000000000000000000000a",
		"0000000000000000000000000g",
		"00000000000000000000000010",
		"7zzzzzzzzzzzzzzzzzzzzzzzzz",
		"prefix_0123456789abcdefghjkmnpqrs",
		"prefix_01h455vb4pex5vsknk084sn02q",
		"pre_fix_00000000000000000000000000",
	}
	invalid := []string{
		"PREFIX_00000000000000000000000000",
		"12345_00000000000000000000000000",
		"pre.fix_00000000000000000000000000",
		"préfix_00000000000000000000000000",
		"  prefix_00000000000000000000000000",
		"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl_00000000000000000000000000",
		"_00000000000000000000000000",
		"_",
		"prefix_1234567890123456789012345",
		"prefix_123456789012345678901234567",
		"prefix_1234567890123456789012345 ",
		"prefix_0123456789ABCDEFGHJKMNPQRS",
		"prefix_123456789-123456789-123456",
		"prefix_ooooooiiiiiiuuuuuuulllllll",
		"prefix_i23456789ol23456789oi23456",
		"prefix_123456789-0123456789-0123456",
		"prefix_8zzzzzzzzzzzzzzzzzzzzzzzzz",
		"_prefix_00000000000000000000000000",
		"prefix__00000000000000000000000000",
		"",
		"prefix_",
	}

	for _, input := range valid {
		id, err := identifiertypeid.Parse(input)
		if err != nil || id.String() != input {
			t.Errorf("valid official vector %q = %q, %v", input, id, err)
		}
	}
	for _, input := range invalid {
		if _, err := identifiertypeid.Parse(input); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("invalid official vector %q error = %v", input, err)
		}
	}
}

func TestKSUIDParserExhaustsDifferentialSingleByteMutations(t *testing.T) {
	const canonical = "0ujtsYcgvSTl8PAuAdqWYSMnLOv"
	for position := range len(canonical) {
		for candidate := 0; candidate <= 255; candidate++ {
			mutated := []byte(canonical)
			mutated[position] = byte(candidate)
			input := string(mutated)
			ours, oursErr := identifierksuid.Parse(input)
			theirs, theirsErr := segmentksuid.Parse(input)
			wantValid := theirsErr == nil && theirs.String() == input
			if (oursErr == nil) != wantValid {
				t.Fatalf("position %d byte 0x%02x reference=%v ours=%v", position, candidate, theirsErr, oursErr)
			}
			if oursErr == nil && ours.String() != theirs.String() {
				t.Fatalf("differential value mismatch: %s != %s", ours, theirs)
			}
		}
	}
}

func TestNanoIDParserExhaustsSingleByteMutations(t *testing.T) {
	const canonical = "_____________________"
	for position := range len(canonical) {
		for candidate := 0; candidate <= 255; candidate++ {
			mutated := []byte(canonical)
			mutated[position] = byte(candidate)
			_, err := identifiernanoid.Parse(string(mutated))
			wantValid := strings.ContainsRune(identifiernanoid.DefaultAlphabet, rune(candidate))
			if (err == nil) != wantValid {
				t.Fatalf("position %d byte 0x%02x valid=%v error=%v", position, candidate, wantValid, err)
			}
		}
	}
}

func TestEveryParserRejectsUnicodeAndNonCanonicalCase(t *testing.T) {
	inputs := []struct {
		name  string
		parse func(string) error
		text  string
	}{
		{name: "UUID Unicode", parse: parseUUID, text: "017f22e2-79b0-7cc3-98c4-dc0c0c0739é"},
		{name: "UUID case", parse: parseUUID, text: "017F22E2-79B0-7CC3-98C4-DC0C0C07398F"},
		{name: "ULID Unicode", parse: parseULID, text: "01ARZ3NDEKTSV4RRFFQ69G5FAé"},
		{name: "ULID case", parse: parseULID, text: "01arz3ndektsv4rrffq69g5fav"},
		{name: "TypeID Unicode", parse: parseTypeID, text: "préfix_01h455vb4pex5vsknk084sn02q"},
		{name: "TypeID case", parse: parseTypeID, text: "prefix_01H455VB4PEX5VSKNK084SN02Q"},
		{name: "KSUID Unicode", parse: parseKSUID, text: "0ujtsYcgvSTl8PAuAdqWYSMnLOé"},
		{name: "NanoID Unicode", parse: parseNanoID, text: "____________________é"},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			if err := input.parse(input.text); !errors.Is(err, identifier.ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func parseUUID(text string) error   { _, err := identifieruuid.Parse(text); return err }
func parseULID(text string) error   { _, err := identifierulid.Parse(text); return err }
func parseTypeID(text string) error { _, err := identifiertypeid.Parse(text); return err }
func parseKSUID(text string) error  { _, err := identifierksuid.Parse(text); return err }
func parseNanoID(text string) error { _, err := identifiernanoid.Parse(text); return err }
