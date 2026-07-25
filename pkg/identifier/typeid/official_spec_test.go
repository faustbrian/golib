package typeid_test

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
)

// The fixtures are copied byte-for-byte from jetify-com/typeid at the
// revision recorded in specification/vector-provenance.tsv.
//
//go:embed testdata/official/valid.json
var officialValidJSON []byte

//go:embed testdata/official/invalid.json
var officialInvalidJSON []byte

type officialValidVector struct {
	Name   string `json:"name"`
	TypeID string `json:"typeid"`
	Prefix string `json:"prefix"`
	UUID   string `json:"uuid"`
}

type officialInvalidVector struct {
	Name   string `json:"name"`
	TypeID string `json:"typeid"`
}

func TestOfficialValidCorpus(t *testing.T) {
	var vectors []officialValidVector
	if err := json.Unmarshal(officialValidJSON, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 9 {
		t.Fatalf("official valid corpus contains %d vectors, want 9", len(vectors))
	}

	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			value := officialUUIDBytes(t, vector.UUID)

			parsed, err := identifiertypeid.Parse(vector.TypeID)
			if err != nil {
				t.Fatalf("Parse(%q): %v", vector.TypeID, err)
			}
			fromBytes, err := identifiertypeid.FromBytes(vector.Prefix, value)
			if err != nil {
				t.Fatalf("FromBytes(%q, %x): %v", vector.Prefix, value, err)
			}
			fromUUID, err := identifiertypeid.FromUUID(vector.Prefix, vector.UUID)
			if err != nil {
				t.Fatalf("FromUUID(%q, %q): %v", vector.Prefix, vector.UUID, err)
			}

			for constructor, id := range map[string]identifiertypeid.ID{
				"Parse": parsed, "FromBytes": fromBytes, "FromUUID": fromUUID,
			} {
				if id.String() != vector.TypeID || id.Prefix() != vector.Prefix || id.Bytes() != value {
					t.Errorf("%s() = %q, %q, %x", constructor, id.String(), id.Prefix(), id.Bytes())
				}
			}
			if parsed != fromBytes || parsed != fromUUID {
				t.Fatal("official constructors produced structurally different TypeIDs")
			}
		})
	}
}

func TestOfficialInvalidCorpus(t *testing.T) {
	var vectors []officialInvalidVector
	if err := json.Unmarshal(officialInvalidJSON, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 21 {
		t.Fatalf("official invalid corpus contains %d vectors, want 21", len(vectors))
	}

	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			if parsed, err := identifiertypeid.Parse(vector.TypeID); err == nil {
				t.Fatalf("Parse(%q) accepted invalid TypeID %q", vector.TypeID, parsed.String())
			}
		})
	}
}

func TestOfficialZeroTypeIDSemantics(t *testing.T) {
	const zero = "00000000000000000000000000"

	parsed, err := identifiertypeid.Parse(zero)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != (identifiertypeid.ID{}) || !parsed.IsZero() || parsed.String() != zero {
		t.Fatalf("zero TypeID = %#v, IsZero %t, String %q", parsed, parsed.IsZero(), parsed.String())
	}
	text, err := parsed.MarshalText()
	if err != nil || string(text) != zero {
		t.Fatalf("zero MarshalText() = %q, %v", text, err)
	}
}

func officialUUIDBytes(t *testing.T, text string) [16]byte {
	t.Helper()
	decoded, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	var value [16]byte
	copy(value[:], decoded)

	return value
}
