package identifier_test

import (
	"testing"

	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func TestReturnedBinaryRepresentationsNeverAliasIdentifiers(t *testing.T) {
	uuidValue, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	ulidValue, _ := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	ksuidValue, _ := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")

	values := []struct {
		name   string
		text   string
		binary func() ([]byte, error)
		format func() string
	}{
		{name: "UUID", text: uuidValue.String(), binary: uuidValue.MarshalBinary, format: uuidValue.String},
		{name: "ULID", text: ulidValue.String(), binary: ulidValue.MarshalBinary, format: ulidValue.String},
		{name: "KSUID", text: ksuidValue.String(), binary: ksuidValue.MarshalBinary, format: ksuidValue.String},
	}

	for _, value := range values {
		t.Run(value.name, func(t *testing.T) {
			first, err := value.binary()
			if err != nil {
				t.Fatal(err)
			}
			first[0] ^= 0xff
			second, err := value.binary()
			if err != nil {
				t.Fatal(err)
			}
			if first[0] == second[0] || value.format() != value.text {
				t.Fatalf("binary mutation aliased %s", value.name)
			}
		})
	}
}
