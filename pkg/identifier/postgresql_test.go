package identifier_test

import (
	"bytes"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgreSQLNativeUUIDBinaryRoundTrip(t *testing.T) {
	original, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	typeMap := pgtype.NewMap()

	encoded, err := typeMap.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes := original.Bytes()
	if len(encoded) != 16 || !bytes.Equal(encoded, originalBytes[:]) {
		t.Fatalf("PostgreSQL UUID binary = %x", encoded)
	}

	var decoded identifieruuid.ID
	if err := typeMap.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("PostgreSQL UUID round trip = %s", decoded)
	}
}

func TestPostgreSQLTextStorageRoundTrips(t *testing.T) {
	typeMap := pgtype.NewMap()
	values := []struct {
		name string
		text string
		scan func([]byte) error
	}{
		{
			name: "ULID",
			text: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			scan: func(encoded []byte) error {
				var value identifierulid.ID
				if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &value); err != nil {
					return err
				}
				if value.String() != string(encoded) {
					t.Fatalf("ULID changed to %s", value)
				}
				return nil
			},
		},
		{
			name: "TypeID",
			text: "prefix_01h455vb4pex5vsknk084sn02q",
			scan: func(encoded []byte) error {
				var value identifiertypeid.ID
				if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &value); err != nil {
					return err
				}
				if value.String() != string(encoded) {
					t.Fatalf("TypeID changed to %s", value)
				}
				return nil
			},
		},
		{
			name: "KSUID",
			text: "0ujtsYcgvSTl8PAuAdqWYSMnLOv",
			scan: func(encoded []byte) error {
				var value identifierksuid.ID
				if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &value); err != nil {
					return err
				}
				if value.String() != string(encoded) {
					t.Fatalf("KSUID changed to %s", value)
				}
				return nil
			},
		},
		{
			name: "NanoID",
			text: "_____________________",
			scan: func(encoded []byte) error {
				var value identifiernanoid.ID
				if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &value); err != nil {
					return err
				}
				if value.String() != string(encoded) {
					t.Fatalf("NanoID changed to %s", value)
				}
				return nil
			},
		},
	}

	for _, value := range values {
		t.Run(value.name, func(t *testing.T) {
			encoded, err := typeMap.Encode(
				pgtype.TextOID, pgtype.TextFormatCode, value.text, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != value.text {
				t.Fatalf("PostgreSQL text changed to %q", encoded)
			}
			if err := value.scan(encoded); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgreSQLTypedWrapperRoundTrip(t *testing.T) {
	const text = "017f22e2-79b0-7cc3-98c4-dc0c0c07398f"
	original, err := identifier.Parse[uuidTag](text)
	if err != nil {
		t.Fatal(err)
	}

	typeMap := pgtype.NewMap()
	encoded, err := typeMap.Encode(pgtype.TextOID, pgtype.TextFormatCode, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded identifier.ID[uuidTag]
	if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("PostgreSQL typed wrapper = %s", decoded)
	}
}
