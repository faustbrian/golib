package internationalpgx_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/internationalpgx"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRegisterSupportsPgxTextEncodeAndScan(t *testing.T) {
	t.Parallel()
	typeMap := pgtype.NewMap()
	internationalpgx.Register(typeMap)
	internationalpgx.Register(nil)

	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatal(err)
	}
	dataType, ok := typeMap.TypeForValue(finland)
	if !ok || dataType == nil || dataType.Name != "text" {
		t.Fatalf("type = %#v, %v", dataType, ok)
	}
	encoded, err := typeMap.Encode(pgtype.TextOID, pgtype.TextFormatCode, finland, nil)
	if err != nil || string(encoded) != "FI" {
		t.Fatalf("Encode() = %q, %v", encoded, err)
	}
	var decoded country.Code
	if err := typeMap.Scan(pgtype.TextOID, pgtype.TextFormatCode, encoded, &decoded); err != nil || decoded != finland {
		t.Fatalf("Scan() = %q, %v", decoded, err)
	}

	number, err := phone.ParseE164("+16502530000")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = typeMap.Encode(pgtype.TextOID, pgtype.TextFormatCode, number, nil)
	if err != nil || string(encoded) != number.E164() {
		t.Fatalf("phone Encode() = %q, %v", encoded, err)
	}
}
