package identifier_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestPinnedCrossLanguageVectors(t *testing.T) {
	t.Run("UUID RFC 9562 and Python", func(t *testing.T) {
		vectors := []struct {
			text    string
			version int
		}{
			{text: "c232ab00-9414-11ec-b3c8-9f6bdeced846", version: 1},
			{text: "5df41881-3aed-3515-88a7-2f4a814cf09e", version: 3},
			{text: "919108f7-52d1-4320-9bac-f847db4148a8", version: 4},
			{text: "2ed6657d-e927-568b-95e1-2665a8aea6a2", version: 5},
			{text: "1ec9414c-232a-6b00-b3c8-9f6bdeced846", version: 6},
			{text: "017f22e2-79b0-7cc3-98c4-dc0c0c07398f", version: 7},
		}
		for _, vector := range vectors {
			id, err := identifieruuid.Parse(vector.text)
			if err != nil || id.Version() != vector.version || id.String() != vector.text {
				t.Fatalf("UUID vector = %s v%d, %v", id, id.Version(), err)
			}
		}
		id, _ := identifieruuid.Parse(vectors[len(vectors)-1].text)
		if id.Inspect().Timestamp.UnixMilli() != 1645557742000 {
			t.Fatalf("UUIDv7 timestamp = %s", id.Inspect().Timestamp)
		}
	})

	t.Run("ULID JavaScript", func(t *testing.T) {
		id, err := identifierulid.Parse("01ARYZ6S41YYYYYYYYYYYYYYYY")
		if err != nil || id.Inspect().Timestamp.UnixMilli() != 1469918176385 {
			t.Fatalf("JavaScript ULID vector = %s, %v", id, err)
		}
	})

	t.Run("TypeID shared specification", func(t *testing.T) {
		id, err := identifiertypeid.Parse("prefix_0123456789abcdefghjkmnpqrs")
		if err != nil || id.String() != "prefix_0123456789abcdefghjkmnpqrs" {
			t.Fatalf("shared TypeID vector = %s, %v", id, err)
		}
	})

	t.Run("KSUID Rust", func(t *testing.T) {
		id, err := identifierksuid.Parse("000000pryYUMiBILyxOCoroLz6w")
		payload, _ := hex.DecodeString("1B7D20E59156E80C7AAD50C707CBD4FA")
		value := id.Bytes()
		if err != nil || id.Inspect().Timestamp.Unix() != 1400000000 ||
			!bytes.Equal(value[4:], payload) {
			t.Fatalf("Rust KSUID vector = %s, %x, %v", id, value, err)
		}
	})

	t.Run("NanoID JavaScript", func(t *testing.T) {
		generator, err := identifiernanoid.NewGenerator(
			identifiernanoid.DefaultConfig(), bytes.NewReader(make([]byte, 64)),
		)
		if err != nil {
			t.Fatal(err)
		}
		id, err := generator.New()
		if err != nil || id.String() != "_____________________" {
			t.Fatalf("JavaScript NanoID vector = %s, %v", id, err)
		}
	})
}
