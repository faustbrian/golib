package identifier_test

import (
	"bytes"
	"sort"
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
	oklogulid "github.com/oklog/ulid/v2"
	segmentksuid "github.com/segmentio/ksuid"
	jetifytypeid "go.jetify.com/typeid/v2"
)

var benchmarkStringSink string

func BenchmarkComparativeParsing(b *testing.B) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{name: "UUID/identifier", parse: func() error {
			_, err := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
			return err
		}},
		{name: "UUID/google", parse: func() error {
			_, err := googleuuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
			return err
		}},
		{name: "ULID/identifier", parse: func() error {
			_, err := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
			return err
		}},
		{name: "ULID/oklog", parse: func() error {
			_, err := oklogulid.ParseStrict("01ARZ3NDEKTSV4RRFFQ69G5FAV")
			return err
		}},
		{name: "TypeID/identifier", parse: func() error {
			_, err := identifiertypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
			return err
		}},
		{name: "TypeID/jetify", parse: func() error {
			_, err := jetifytypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
			return err
		}},
		{name: "KSUID/identifier", parse: func() error {
			_, err := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
			return err
		}},
		{name: "KSUID/segment", parse: func() error {
			_, err := segmentksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
			return err
		}},
		{name: "NanoID/identifier", parse: func() error {
			_, err := identifiernanoid.Parse("_____________________")
			return err
		}},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for b.Loop() {
				if err := test.parse(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkComparativeFormatting(b *testing.B) {
	uuidOurs, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	uuidTheirs, _ := googleuuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	ulidOurs, _ := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	ulidTheirs, _ := oklogulid.ParseStrict("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	typeIDOurs, _ := identifiertypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
	typeIDTheirs, _ := jetifytypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q")
	ksuidOurs, _ := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
	ksuidTheirs, _ := segmentksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")

	tests := []struct {
		name   string
		format func() string
	}{
		{name: "UUID/identifier", format: uuidOurs.String},
		{name: "UUID/google", format: uuidTheirs.String},
		{name: "ULID/identifier", format: ulidOurs.String},
		{name: "ULID/oklog", format: ulidTheirs.String},
		{name: "TypeID/identifier", format: typeIDOurs.String},
		{name: "TypeID/jetify", format: typeIDTheirs.String},
		{name: "KSUID/identifier", format: ksuidOurs.String},
		{name: "KSUID/segment", format: ksuidTheirs.String},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for b.Loop() {
				benchmarkStringSink = test.format()
			}
		})
	}
}

func BenchmarkComparativeSorting(b *testing.B) {
	instant := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	const count = 1024
	uuidValues := generateValues(b, identifieruuid.NewV7Generator(
		idtest.NewClock(instant), idtest.NewReader([]byte("benchmark-uuid")),
	), count)
	ulidValues := generateValues(b, identifierulid.NewGenerator(
		idtest.NewClock(instant), idtest.NewReader([]byte("benchmark-ulid")),
	), count)
	ksuidValues := generateValues(b, identifierksuid.NewGenerator(
		idtest.NewClock(instant), idtest.NewReader([]byte("benchmark-ksuid")),
	), count)

	googleValues := make([]googleuuid.UUID, count)
	oklogValues := make([]oklogulid.ULID, count)
	segmentValues := make([]segmentksuid.KSUID, count)
	for index := range count {
		googleValues[index] = googleuuid.UUID(uuidValues[index].Bytes())
		oklogValues[index] = oklogulid.ULID(ulidValues[index].Bytes())
		segmentValues[index] = segmentksuid.KSUID(ksuidValues[index].Bytes())
	}
	reverse(uuidValues)
	reverse(googleValues)
	reverse(ulidValues)
	reverse(oklogValues)
	reverse(ksuidValues)
	reverse(segmentValues)

	benchmarkSort(b, "UUID/identifier", uuidValues, func(left, right identifieruuid.ID) bool {
		return left.Compare(right) < 0
	})
	benchmarkSort(b, "UUID/google", googleValues, func(left, right googleuuid.UUID) bool {
		return bytes.Compare(left[:], right[:]) < 0
	})
	benchmarkSort(b, "ULID/identifier", ulidValues, func(left, right identifierulid.ID) bool {
		return left.Compare(right) < 0
	})
	benchmarkSort(b, "ULID/oklog", oklogValues, func(left, right oklogulid.ULID) bool {
		return left.Compare(right) < 0
	})
	benchmarkSort(b, "KSUID/identifier", ksuidValues, func(left, right identifierksuid.ID) bool {
		return left.Compare(right) < 0
	})
	benchmarkSort(b, "KSUID/segment", segmentValues, func(left, right segmentksuid.KSUID) bool {
		return bytes.Compare(left[:], right[:]) < 0
	})
}

func BenchmarkDatabaseLocalityProxy(b *testing.B) {
	instant := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	const count = 4096
	randomUUIDs := generateStrings(b,
		identifieruuid.NewV4Generator(idtest.NewReader([]byte("locality-v4"))), count,
	)
	orderedUUIDs := generateStrings(b, identifieruuid.NewV7Generator(
		idtest.NewClock(instant), idtest.NewReader([]byte("locality-v7")),
	), count)
	orderedULIDs := generateStrings(b, identifierulid.NewGenerator(
		idtest.NewClock(instant), idtest.NewReader([]byte("locality-ulid")),
	), count)
	orderedKSUIDs := generateStrings(b, identifierksuid.NewGenerator(
		idtest.NewClock(instant), idtest.NewReader([]byte("locality-ksuid")),
	), count)

	benchmarkSearchInsertPosition(b, "UUIDv4/random", randomUUIDs)
	benchmarkSearchInsertPosition(b, "UUIDv7/ordered", orderedUUIDs)
	benchmarkSearchInsertPosition(b, "ULID/ordered", orderedULIDs)
	benchmarkSearchInsertPosition(b, "KSUID/ordered", orderedKSUIDs)
}

func BenchmarkNanoIDGenerationAgainstMaintainedImplementation(b *testing.B) {
	b.Run("identifier", func(b *testing.B) {
		generator, _ := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), nil)
		for b.Loop() {
			value, err := generator.New()
			if err != nil {
				b.Fatal(err)
			}
			benchmarkStringSink = value.String()
		}
	})
	b.Run("matoous", func(b *testing.B) {
		for b.Loop() {
			value, err := gonanoid.New()
			if err != nil {
				b.Fatal(err)
			}
			benchmarkStringSink = value
		}
	})
}

type generatedString interface {
	String() string
}

type benchmarkGenerator[T any] interface {
	New() (T, error)
}

func generateValues[T any](b *testing.B, generator benchmarkGenerator[T], count int) []T {
	b.Helper()
	values := make([]T, count)
	for index := range count {
		value, err := generator.New()
		if err != nil {
			b.Fatal(err)
		}
		values[index] = value
	}

	return values
}

func generateStrings[T generatedString](
	b *testing.B,
	generator benchmarkGenerator[T],
	count int,
) []string {
	b.Helper()
	values := generateValues(b, generator, count)
	text := make([]string, count)
	for index := range count {
		text[index] = values[index].String()
	}

	return text
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func benchmarkSort[T any](b *testing.B, name string, source []T, less func(T, T) bool) {
	b.Run(name, func(b *testing.B) {
		working := make([]T, len(source))
		for b.Loop() {
			copy(working, source)
			sort.Slice(working, func(left, right int) bool {
				return less(working[left], working[right])
			})
		}
	})
}

func benchmarkSearchInsertPosition(b *testing.B, name string, keys []string) {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	b.Run(name, func(b *testing.B) {
		for index := 0; b.Loop(); index++ {
			key := keys[index%len(keys)]
			position := sort.SearchStrings(sorted, key)
			if position == len(sorted) {
				b.Fatal("key was not found")
			}
		}
	})
}
