package identifier_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

type uuidTag struct{}

func (uuidTag) Validate(text string) error {
	_, err := identifieruuid.Parse(text)

	return err
}

func TestIdentifiersAreRedactedByStructuredLogging(t *testing.T) {
	typed, err := identifier.Parse[uuidTag]("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	if err != nil {
		t.Fatal(err)
	}
	uuidValue, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	ulidValue, _ := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	typeIDValue, _ := identifiertypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
	ksuidValue, _ := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
	nanoIDValue, _ := identifiernanoid.Parse("_____________________")

	values := []struct {
		name string
		raw  string
		id   any
	}{
		{name: "typed", raw: typed.String(), id: typed},
		{name: "UUID", raw: uuidValue.String(), id: uuidValue},
		{name: "ULID", raw: ulidValue.String(), id: ulidValue},
		{name: "TypeID", raw: typeIDValue.String(), id: typeIDValue},
		{name: "KSUID", raw: ksuidValue.String(), id: ksuidValue},
		{name: "NanoID", raw: nanoIDValue.String(), id: nanoIDValue},
	}

	for _, value := range values {
		t.Run(value.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			logger.LogAttrs(context.Background(), slog.LevelInfo, "generated",
				slog.Any("identifier", value.id))

			if strings.Contains(output.String(), value.raw) {
				t.Fatalf("structured log exposed identifier: %s", output.String())
			}
			if !strings.Contains(output.String(), "[REDACTED]") {
				t.Fatalf("structured log did not mark redaction: %s", output.String())
			}
		})
	}
}
