package identifier_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	"github.com/faustbrian/golib/pkg/identifier/idtest"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifiertypeid "github.com/faustbrian/golib/pkg/identifier/typeid"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

type chunkReader struct {
	reader *bytes.Reader
	limit  int
}

func (reader *chunkReader) Read(output []byte) (int, error) {
	if len(output) > reader.limit {
		output = output[:reader.limit]
	}

	return reader.reader.Read(output)
}

func TestGeneratorsAcceptProgressiveShortReadsAndRejectPrematureEOF(t *testing.T) {
	instant := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	short := func(size int) io.Reader {
		return &chunkReader{reader: bytes.NewReader(make([]byte, size)), limit: 1}
	}

	tests := []struct {
		name    string
		success func() error
		failure func() error
	}{
		{
			name: "UUIDv4",
			success: func() error {
				_, err := identifieruuid.NewV4Generator(short(16)).New()
				return err
			},
			failure: func() error {
				_, err := identifieruuid.NewV4Generator(bytes.NewReader(make([]byte, 15))).New()
				return err
			},
		},
		{
			name: "UUIDv7",
			success: func() error {
				_, err := identifieruuid.NewV7Generator(idtest.NewClock(instant), short(10)).New()
				return err
			},
			failure: func() error {
				_, err := identifieruuid.NewV7Generator(
					idtest.NewClock(instant), bytes.NewReader(make([]byte, 9)),
				).New()
				return err
			},
		},
		{
			name: "ULID",
			success: func() error {
				_, err := identifierulid.NewGenerator(idtest.NewClock(instant), short(10)).New()
				return err
			},
			failure: func() error {
				_, err := identifierulid.NewGenerator(
					idtest.NewClock(instant), bytes.NewReader(make([]byte, 9)),
				).New()
				return err
			},
		},
		{
			name: "TypeID",
			success: func() error {
				generator, err := identifiertypeid.NewGenerator("short", identifieruuid.NewV7Generator(
					idtest.NewClock(instant), short(10),
				))
				if err != nil {
					return err
				}
				_, err = generator.New()
				return err
			},
			failure: func() error {
				generator, err := identifiertypeid.NewGenerator("short", identifieruuid.NewV7Generator(
					idtest.NewClock(instant), bytes.NewReader(make([]byte, 9)),
				))
				if err != nil {
					return err
				}
				_, err = generator.New()
				return err
			},
		},
		{
			name: "KSUID",
			success: func() error {
				_, err := identifierksuid.NewGenerator(idtest.NewClock(instant), short(16)).New()
				return err
			},
			failure: func() error {
				_, err := identifierksuid.NewGenerator(
					idtest.NewClock(instant), bytes.NewReader(make([]byte, 15)),
				).New()
				return err
			},
		},
		{
			name: "NanoID",
			success: func() error {
				generator, err := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), short(64))
				if err != nil {
					return err
				}
				_, err = generator.New()
				return err
			},
			failure: func() error {
				generator, err := identifiernanoid.NewGenerator(
					identifiernanoid.DefaultConfig(), bytes.NewReader([]byte{0}),
				)
				if err != nil {
					return err
				}
				_, err = generator.New()
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.success(); err != nil {
				t.Fatalf("progressive short reads failed: %v", err)
			}
			if err := test.failure(); !errors.Is(err, identifier.ErrEntropy) {
				t.Fatalf("premature EOF error = %v", err)
			}
		})
	}
}

func TestDuplicatedGeneratorStateDefinesACollisionDomain(t *testing.T) {
	instant := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	assertDuplicate := func(t *testing.T, left, right identifier.Generator[string]) {
		t.Helper()
		first, err := left.New()
		if err != nil {
			t.Fatal(err)
		}
		second, err := right.New()
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatalf("duplicated deterministic state diverged: %q != %q", first, second)
		}
	}

	t.Run("UUIDv4", func(t *testing.T) {
		assertDuplicate(t,
			stringGenerator[identifieruuid.ID]{identifieruuid.NewV4Generator(idtest.NewReader([]byte("clone")))},
			stringGenerator[identifieruuid.ID]{identifieruuid.NewV4Generator(idtest.NewReader([]byte("clone")))},
		)
	})
	t.Run("UUIDv7", func(t *testing.T) {
		assertDuplicate(t,
			stringGenerator[identifieruuid.ID]{identifieruuid.NewV7Generator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
			stringGenerator[identifieruuid.ID]{identifieruuid.NewV7Generator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
		)
	})
	t.Run("ULID", func(t *testing.T) {
		assertDuplicate(t,
			stringGenerator[identifierulid.ID]{identifierulid.NewGenerator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
			stringGenerator[identifierulid.ID]{identifierulid.NewGenerator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
		)
	})
	t.Run("KSUID", func(t *testing.T) {
		assertDuplicate(t,
			stringGenerator[identifierksuid.ID]{identifierksuid.NewGenerator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
			stringGenerator[identifierksuid.ID]{identifierksuid.NewGenerator(idtest.NewClock(instant), idtest.NewReader([]byte("clone")))},
		)
	})
	t.Run("NanoID", func(t *testing.T) {
		left, _ := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), idtest.NewReader([]byte("clone")))
		right, _ := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), idtest.NewReader([]byte("clone")))
		assertDuplicate(t, stringGenerator[identifiernanoid.ID]{left}, stringGenerator[identifiernanoid.ID]{right})
	})
}

type stringGenerator[T interface{ String() string }] struct {
	generator identifier.Generator[T]
}

func (generator stringGenerator[T]) New() (string, error) {
	value, err := generator.generator.New()
	if err != nil {
		return "", err
	}

	return value.String(), nil
}
