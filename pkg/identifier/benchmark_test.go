package identifier_test

import (
	cryptorand "crypto/rand"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
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

func BenchmarkGeneration(b *testing.B) {
	b.Run("UUIDv4/identifier", func(b *testing.B) {
		generator := identifieruuid.NewV4Generator(nil)
		for b.Loop() {
			_, _ = generator.New()
		}
	})
	b.Run("UUIDv4/google", func(b *testing.B) {
		for b.Loop() {
			_, _ = googleuuid.NewRandom()
		}
	})
	b.Run("ULID/identifier", func(b *testing.B) {
		generator := identifierulid.NewGenerator(nil, nil)
		for b.Loop() {
			_, _ = generator.New()
		}
	})
	b.Run("ULID/oklog", func(b *testing.B) {
		for b.Loop() {
			_, _ = oklogulid.New(oklogulid.Timestamp(time.Now()), cryptorand.Reader)
		}
	})
	b.Run("TypeID/identifier", func(b *testing.B) {
		generator, _ := identifiertypeid.NewGenerator("user", identifieruuid.NewV7Generator(nil, nil))
		for b.Loop() {
			_, _ = generator.New()
		}
	})
	b.Run("TypeID/jetify", func(b *testing.B) {
		for b.Loop() {
			_, _ = jetifytypeid.Generate("user")
		}
	})
	b.Run("KSUID/identifier", func(b *testing.B) {
		generator := identifierksuid.NewGenerator(nil, nil)
		for b.Loop() {
			_, _ = generator.New()
		}
	})
	b.Run("KSUID/segment", func(b *testing.B) {
		for b.Loop() {
			_, _ = segmentksuid.NewRandom()
		}
	})
	b.Run("NanoID/identifier", func(b *testing.B) {
		generator, _ := identifiernanoid.NewGenerator(identifiernanoid.DefaultConfig(), nil)
		for b.Loop() {
			_, _ = generator.New()
		}
	})
	b.Run("NanoID/matoous", func(b *testing.B) {
		for b.Loop() {
			_, _ = gonanoid.New()
		}
	})
}

func BenchmarkParsing(b *testing.B) {
	benchmarks := []struct {
		name  string
		parse func() error
	}{
		{"UUID", func() error { _, err := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f"); return err }},
		{"ULID", func() error { _, err := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV"); return err }},
		{"TypeID", func() error { _, err := identifiertypeid.Parse("prefix_01h455vb4pex5vsknk084sn02q"); return err }},
		{"KSUID", func() error { _, err := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv"); return err }},
		{"NanoID", func() error { _, err := identifiernanoid.Parse("_____________________"); return err }},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			for b.Loop() {
				if err := benchmark.parse(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func ExampleID() {
	// Domain tags validate canonical family text without reflection or a
	// runtime registry. See the package documentation for a complete tag.
	var _ identifier.Generator[identifieruuid.ID] = identifieruuid.NewV4Generator(nil)
}
