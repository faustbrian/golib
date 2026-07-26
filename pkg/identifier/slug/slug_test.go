package slug

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestLaravelEnglishMatchesFrozenLaravelDifferentialVectors(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/laravel_english.json")
	if err != nil {
		t.Fatalf("read Laravel differential vectors: %v", err)
	}
	var vectors []struct {
		Source   string `json:"source"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(contents, &vectors); err != nil {
		t.Fatalf("decode Laravel differential vectors: %v", err)
	}
	if len(vectors) < 1_000 {
		t.Fatalf("Laravel differential vector count = %d, want at least 1,000", len(vectors))
	}
	for _, vector := range vectors {
		if actual := LaravelEnglish(vector.Source); actual != vector.Expected {
			t.Fatalf(
				"LaravelEnglish(%q) = %q, want %q",
				vector.Source,
				actual,
				vector.Expected,
			)
		}
	}
}

func TestLaravelEnglishPreservesFrozenLaravelSlugOutputs(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Posti":                "posti",
		"DB Schenker":          "db-schenker",
		"Åland Post":           "aland-post",
		"Äänekoski Öö":         "aanekoski-oo",
		"DHL_Freight---FI":     "dhl-freight-fi",
		"Carrier@example.com":  "carrier-at-examplecom",
		"  spaced   carrier  ": "spaced-carrier",
		"Crème brûlée":         "creme-brulee",
		"Straße":               "strasse",
		"Æsir & Øresund":       "aesir-oresund",
		"хелло ворлд":          "xello-vorld",
		"ΑΥ":                   "au",
		"A̧land":               "aland",
		"影師":                   "",
		"emoji 🚚 carrier":      "emoji-carrier",
		"___":                  "",
		"":                     "",
		"mixed\x00control\tid": "mixedcontrol-id",
	}
	for source, expected := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			if actual := LaravelEnglish(source); actual != expected {
				t.Fatalf(
					"LaravelEnglish(%q) = %q, want %q",
					source,
					actual,
					expected,
				)
			}
		})
	}
}

func TestLaravelEnglishTruncatesTheSourceBeforeTransliteration(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("å", MaxSourceRunes) + " suffix"
	actual := LaravelEnglish(source)
	if utf8.RuneCountInString(actual) != MaxSourceRunes {
		t.Fatalf(
			"slug rune count = %d, want %d",
			utf8.RuneCountInString(actual),
			MaxSourceRunes,
		)
	}
	if strings.Contains(actual, "suffix") {
		t.Fatalf("slug retained text after source limit: %q", actual)
	}
}

func TestLaravelEnglishIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const workers = 32
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	failures := make(chan string, workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			if actual := LaravelEnglish("Åland @ Post"); actual != "aland-at-post" {
				failures <- actual
			}
		}()
	}
	waitGroup.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("concurrent slug = %q", failure)
	}
}

func FuzzLaravelEnglish(f *testing.F) {
	for _, seed := range []string{
		"Åland Post",
		"Carrier@example.com",
		"ΑΥ A̧land",
		"影師 🚚",
		"\x00\xff",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, source string) {
		actual := LaravelEnglish(source)
		if actual != LaravelEnglish(source) {
			t.Fatalf("non-deterministic slug for %q", source)
		}
		if strings.Trim(actual, "-") != actual {
			t.Fatalf("slug has a boundary separator: %q", actual)
		}
		if strings.Contains(actual, "--") {
			t.Fatalf("slug has adjacent separators: %q", actual)
		}
		for _, character := range actual {
			if character == '-' ||
				character >= 'a' && character <= 'z' ||
				character >= '0' && character <= '9' {
				continue
			}
			t.Fatalf("slug contains a non-canonical character: %q", actual)
		}
	})
}
