package identifier_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/identifier/idtest"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
	googleuuid "github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
	jetifytypeid "go.jetify.com/typeid/v2"
)

func TestMaintainedImplementationInteroperability(t *testing.T) {
	t.Run("UUID", func(t *testing.T) {
		const text = "017f22e2-79b0-7cc3-98c4-dc0c0c07398f"
		ours, err := identifieruuid.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		theirs, err := googleuuid.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		oursBytes := ours.Bytes()
		if !bytes.Equal(oursBytes[:], theirs[:]) {
			t.Fatal("Google UUID bytes differ")
		}
	})

	t.Run("TypeID", func(t *testing.T) {
		const text = "prefix_01h455vb4pex5vsknk084sn02q"
		ours, err := identifiertypeid.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		theirs, err := jetifytypeid.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		oursBytes := ours.Bytes()
		if ours.String() != theirs.String() || !bytes.Equal(oursBytes[:], theirs.Bytes()) {
			t.Fatal("Jetify TypeID representation differs")
		}
	})

	t.Run("NanoID", func(t *testing.T) {
		config := identifiernanoid.Config{Alphabet: "ab", Size: 120}
		text, err := gonanoid.Generate(config.Alphabet, config.Size)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := identifiernanoid.ParseWithConfig(text, config); err != nil {
			t.Fatalf("parse reference NanoID: %v", err)
		}
	})
}

func TestFamilyWideCollisionAndOrderingSimulation(t *testing.T) {
	instant := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("UUIDv4", func(t *testing.T) {
		idtest.AssertUnique(t, identifieruuid.NewV4Generator(idtest.NewReader([]byte("uuid-v4"))), 1000)
	})
	t.Run("UUIDv7", func(t *testing.T) {
		values := idtest.AssertUnique(t, identifieruuid.NewV7Generator(
			idtest.NewClock(instant), idtest.NewReader([]byte("uuid-v7")),
		), 1000)
		idtest.AssertStrictlyOrdered(t, values, func(left, right identifieruuid.ID) int {
			return left.Compare(right)
		})
	})
	t.Run("ULID", func(t *testing.T) {
		values := idtest.AssertUnique(t, identifierulid.NewGenerator(
			idtest.NewClock(instant), idtest.NewReader([]byte("ulid")),
		), 1000)
		idtest.AssertStrictlyOrdered(t, values, func(left, right identifierulid.ID) int {
			return left.Compare(right)
		})
	})
	t.Run("TypeID", func(t *testing.T) {
		generator, err := identifiertypeid.NewGenerator("user", identifieruuid.NewV7Generator(
			idtest.NewClock(instant), idtest.NewReader([]byte("typeid")),
		))
		if err != nil {
			t.Fatal(err)
		}
		values := idtest.AssertUnique(t, generator, 1000)
		idtest.AssertStrictlyOrdered(t, values, func(left, right identifiertypeid.ID) int {
			return left.Compare(right)
		})
	})
	t.Run("KSUID", func(t *testing.T) {
		values := idtest.AssertUnique(t, identifierksuid.NewGenerator(
			idtest.NewClock(instant), idtest.NewReader([]byte("ksuid")),
		), 1000)
		idtest.AssertStrictlyOrdered(t, values, func(left, right identifierksuid.ID) int {
			return left.Compare(right)
		})
	})
	t.Run("NanoID", func(t *testing.T) {
		generator, err := identifiernanoid.NewGenerator(
			identifiernanoid.DefaultConfig(), idtest.NewReader([]byte("nanoid")),
		)
		if err != nil {
			t.Fatal(err)
		}
		idtest.AssertUnique(t, generator, 1000)
	})
}
