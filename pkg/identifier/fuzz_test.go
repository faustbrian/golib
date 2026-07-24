package identifier_test

import (
	"encoding"
	"encoding/json"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

func FuzzUUIDParse(f *testing.F) {
	f.Add("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := identifieruuid.Parse(input)
		if err == nil && id.String() != input {
			t.Fatalf("non-canonical success: %q became %q", input, id)
		}
	})
}

func FuzzULIDParse(f *testing.F) {
	f.Add("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := identifierulid.Parse(input)
		if err == nil && id.String() != input {
			t.Fatalf("non-canonical success: %q became %q", input, id)
		}
	})
}

func FuzzTypeIDParse(f *testing.F) {
	f.Add("prefix_01h455vb4pex5vsknk084sn02q")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := identifiertypeid.Parse(input)
		if err == nil && id.String() != input {
			t.Fatalf("non-canonical success: %q became %q", input, id)
		}
	})
}

func FuzzKSUIDParse(f *testing.F) {
	f.Add("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := identifierksuid.Parse(input)
		if err == nil && id.String() != input {
			t.Fatalf("non-canonical success: %q became %q", input, id)
		}
	})
}

func FuzzNanoIDParse(f *testing.F) {
	f.Add("_____________________")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := identifiernanoid.Parse(input)
		if err == nil && id.String() != input {
			t.Fatalf("non-canonical success: %q became %q", input, id)
		}
	})
}

func FuzzBinaryAndJSONCodecs(f *testing.F) {
	f.Add(uint8(0), []byte("017f22e2-79b0-7cc3-98c4-dc0c0c07398f"))
	f.Add(uint8(1), []byte("01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	f.Add(uint8(2), []byte("prefix_01h455vb4pex5vsknk084sn02q"))
	f.Add(uint8(3), []byte("0ujtsYcgvSTl8PAuAdqWYSMnLOv"))
	f.Add(uint8(4), []byte("_____________________"))
	f.Add(uint8(5), []byte(`"017f22e2-79b0-7cc3-98c4-dc0c0c07398f"`))
	f.Fuzz(func(t *testing.T, family uint8, input []byte) {
		switch family % 6 {
		case 0:
			fuzzCodec[identifieruuid.ID](t, input)
		case 1:
			fuzzCodec[identifierulid.ID](t, input)
		case 2:
			fuzzCodec[identifiertypeid.ID](t, input)
		case 3:
			fuzzCodec[identifierksuid.ID](t, input)
		case 4:
			fuzzCodec[identifiernanoid.ID](t, input)
		case 5:
			fuzzCodec[identifier.ID[uuidTag]](t, input)
		}
	})
}

func fuzzCodec[T comparable](t *testing.T, input []byte) {
	t.Helper()

	var binaryValue T
	unmarshaler := any(&binaryValue).(encoding.BinaryUnmarshaler)
	if err := unmarshaler.UnmarshalBinary(input); err == nil {
		encoded, marshalErr := any(binaryValue).(encoding.BinaryMarshaler).MarshalBinary()
		if marshalErr != nil {
			t.Fatalf("marshal accepted binary value: %v", marshalErr)
		}
		var decoded T
		if err := any(&decoded).(encoding.BinaryUnmarshaler).UnmarshalBinary(encoded); err != nil {
			t.Fatalf("decode canonical binary: %v", err)
		}
		if decoded != binaryValue {
			t.Fatal("binary round trip changed value")
		}
	}

	var jsonValue T
	if err := json.Unmarshal(input, &jsonValue); err == nil {
		encoded, marshalErr := json.Marshal(jsonValue)
		if marshalErr != nil {
			t.Fatalf("marshal accepted JSON value: %v", marshalErr)
		}
		var decoded T
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("decode canonical JSON: %v", err)
		}
		if decoded != jsonValue {
			t.Fatal("JSON round trip changed value")
		}
	}
}
