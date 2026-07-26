package identifier_test

import (
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/identifier/idtest"
	identifiernanoid "github.com/faustbrian/golib/pkg/identifier/nanoid"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

type cyclingByteReader struct {
	mutex sync.Mutex
	next  byte
}

func (reader *cyclingByteReader) Read(output []byte) (int, error) {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()

	for index := range output {
		output[index] = reader.next
		reader.next++
	}

	return len(output), nil
}

func TestNanoIDRejectionSamplingHasNoModuloBias(t *testing.T) {
	config := identifiernanoid.Config{
		Alphabet: identifiernanoid.DefaultAlphabet[:63],
		Size:     identifiernanoid.DefaultSize,
	}
	generator, err := identifiernanoid.NewGenerator(config, &cyclingByteReader{})
	if err != nil {
		t.Fatal(err)
	}

	counts := make(map[byte]int, len(config.Alphabet))
	const samples = 6300
	for range samples {
		id, generationErr := generator.New()
		if generationErr != nil {
			t.Fatal(generationErr)
		}
		for index := range len(id.String()) {
			counts[id.String()[index]]++
		}
	}

	minimum := samples * config.Size
	maximum := 0
	for index := range len(config.Alphabet) {
		count := counts[config.Alphabet[index]]
		if count < minimum {
			minimum = count
		}
		if count > maximum {
			maximum = count
		}
	}
	// The deterministic byte cycle and per-call buffer discard introduce a
	// bounded phase effect. A modulo implementation would make four symbols
	// approximately twice as common for a 63-byte alphabet.
	if maximum*100 > minimum*115 {
		t.Fatalf("rejection sampling spread = %d..%d", minimum, maximum)
	}
}

func TestRandomFamilyCollisionCampaign(t *testing.T) {
	const samples = 50_000
	idtest.AssertUnique(t,
		identifieruuid.NewV4Generator(idtest.NewReader([]byte("uuid-v4-collision-campaign"))),
		samples,
	)

	nanoIDGenerator, err := identifiernanoid.NewGenerator(
		identifiernanoid.DefaultConfig(), idtest.NewReader([]byte("nanoid-collision-campaign")),
	)
	if err != nil {
		t.Fatal(err)
	}
	idtest.AssertUnique(t, nanoIDGenerator, samples)
}
