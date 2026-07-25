package http_test

import (
	"testing"

	localizedhttp "github.com/faustbrian/golib/pkg/localized/http"
)

func FuzzParseAcceptLanguage(f *testing.F) {
	for _, seed := range []string{"", "en", "fi;q=0.8,en-US", "*;q=0.5", "en;q=1.001", "en_US"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, header string) {
		preferences, err := localizedhttp.ParseAcceptLanguage(header, localizedhttp.ParseOptions{MaxBytes: 8 << 10, MaxCandidates: 64})
		if err != nil {
			return
		}
		if len(preferences) > 64 {
			t.Fatalf("candidate limit bypass: %d", len(preferences))
		}
		for _, preference := range preferences {
			if preference.Weight < 0 || preference.Weight > 1 {
				t.Fatalf("weight = %v", preference.Weight)
			}
		}
	})
}
