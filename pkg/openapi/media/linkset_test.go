package media_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/media"
)

func TestSerializeLinksetTranscodesRFC9264JSONModel(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{
		"anchor":"https://example.test/chapters",
		"next":[{"href":"https://example.test/two","type":"text/html",
			"hreflang":["en","de"],"title":"Next chapter",
			"title*":[{"value":"nächstes Kapitel","language":"de"}],
			"x-note":["one","two"]}],
		"https://example.test/rels/alternate":[{"href":"/alternate"}]
	}]}`)
	serialized, err := media.SerializeLinkset(value, 2, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	want := `<https://example.test/two>; rel="next"` +
		`; anchor="https://example.test/chapters"` +
		`; type="text/html"; hreflang="en"; hreflang="de"` +
		`; title="Next chapter"` +
		`; title*=UTF-8'de'n%C3%A4chstes%20Kapitel` +
		`; x-note="one"; x-note="two",` + "\n" +
		`</alternate>; rel="https://example.test/rels/alternate"` +
		`; anchor="https://example.test/chapters"`
	if serialized != want {
		t.Fatalf("SerializeLinkset() = %q, want %q", serialized, want)
	}
	if err := media.ValidateLinksetJSON(value, 2, 2_000); err != nil {
		t.Fatalf("ValidateLinksetJSON() error = %v", err)
	}
}

func TestLinksetValidationRejectsInvalidStructures(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`null`,
		`{}`,
		`{"linkset":[],"extra":true}`,
		`{"linkset":{}}`,
		`{"linkset":[null]}`,
		`{"linkset":[{}]}`,
		`{"linkset":[{"anchor":"bad\nuri","next":[{"href":"/ok"}]}]}`,
		`{"linkset":[{"/relative":[{"href":"/ok"}]}]}`,
		`{"linkset":[{" ":[{"href":"/ok"}]}]}`,
		`{"linkset":[{"next":[]}]}`,
		`{"linkset":[{"next":[null]}]}`,
		`{"linkset":[{"next":[{}]}]}`,
		`{"linkset":[{"next":[{"href":"bad\nuri"}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","type":1}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","hreflang":"en"}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","hreflang":[]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","hreflang":[1]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","bad name":["x"]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","x-note":[]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":[]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":["x"]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":[{"language":"en"}]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":[{
			"value":"x","language":"en","extra":"bad"}]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":[{
			"value":"x","extra":"bad"}]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","title*":[{
			"value":"x","language":"é"}]}]}]}`,
		`{"linkset":[{"next":[{"href":"/ok","x-note":[1]}]}]}`,
	} {
		value := mustMediaValue(t, raw)
		if _, err := media.SerializeLinkset(value, 10, 1_000); !errors.Is(
			err, media.ErrInvalidLinkset,
		) {
			t.Fatalf("SerializeLinkset(%s) error = %v", raw, err)
		}
	}
}

func TestSerializeLinksetEnforcesExpandedOutputBound(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{
		"anchor":"https://example.test/a/very/long/shared/context",
		"next":[
			{"href":"/one","title*":[{"value":"nächstes Kapitel"}]},
			{"href":"/two","title*":[{"value":"nächstes Kapitel"}]},
			{"href":"/three","title*":[{"value":"nächstes Kapitel"}]}
		]
	}]}`)
	raw, err := value.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	full, err := media.SerializeLinkset(value, 3, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) <= len(raw) {
		t.Fatalf("test fixture does not expand: JSON=%d, linkset=%d",
			len(raw), len(full))
	}
	if exact, err := media.SerializeLinkset(value, 3, len(full)); err != nil ||
		exact != full {
		t.Fatalf("exact expanded output = %q, %v", exact, err)
	}
	for maximum := len(raw); maximum < len(full); maximum++ {
		if _, err := media.SerializeLinkset(value, 3, maximum); !errors.Is(
			err, media.ErrLinksetLimit,
		) {
			t.Fatalf("maximum %d error = %v", maximum, err)
		}
	}
}

func TestLinksetValidationEnforcesLinkAndByteBounds(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{"next":[
		{"href":"/one"},{"href":"/two"}
	]}]}`)
	for _, test := range []struct {
		links int
		bytes int
		want  error
	}{
		{links: 0, bytes: 1_000, want: media.ErrInvalidLinkset},
		{links: 2, bytes: 0, want: media.ErrInvalidLinkset},
		{links: 1, bytes: 1_000, want: media.ErrLinksetLimit},
		{links: 2, bytes: 5, want: media.ErrLinksetLimit},
	} {
		_, err := media.SerializeLinkset(value, test.links, test.bytes)
		if !errors.Is(err, test.want) {
			t.Fatalf("SerializeLinkset() error = %v, want %v", err, test.want)
		}
	}
	if err := media.ValidateLinksetJSON(value, 1, 1_000); !errors.Is(
		err, media.ErrLinksetLimit,
	) {
		t.Fatalf("ValidateLinksetJSON() error = %v", err)
	}
}

func TestSerializeLinksetBoundsRepeatedContextExpansion(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{
		"anchor":"https://example.test/a/very/long/context/repeated/for/every/link",
		"next":[
			{"href":"/1","title":"one"},{"href":"/2","title":"two"},
			{"href":"/3","title":"three"},{"href":"/4","title":"four"},
			{"href":"/5","title":"five"},{"href":"/6","title":"six"},
			{"href":"/7","title":"seven"},{"href":"/8","title":"eight"}
		]
	}]}`)
	raw, err := value.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	full, err := media.SerializeLinkset(value, 8, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) <= len(raw) {
		t.Fatalf("test fixture does not expand: JSON=%d, linkset=%d",
			len(raw), len(full))
	}
	for maximum := len(raw); maximum < len(full); maximum++ {
		if _, err := media.SerializeLinkset(value, 8, maximum); !errors.Is(
			err, media.ErrLinksetLimit,
		) {
			t.Fatalf("maximum %d error = %v", maximum, err)
		}
	}
}

func TestSerializeLinksetRejectsNonASCIIOrdinaryAttributes(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{"next":[{
		"href":"/ok","title":"nächstes"
	}]}]}`)
	_, err := media.SerializeLinkset(value, 1, 1_000)
	if !errors.Is(err, media.ErrInvalidLinkset) ||
		strings.Contains(err.Error(), "nächstes") {
		t.Fatalf("SerializeLinkset() error = %v", err)
	}
}

func TestLinksetAcceptsEveryExactSyntaxAndResourceBoundary(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{"a":[{
		"href":"/ok","title":" ~","title*":[{"value":"aAzZ09"}]
	}]}]}`)
	raw, err := value.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := media.ValidateLinksetJSON(value, 1, len(raw)); err != nil {
		t.Fatalf("exact validation limits error = %v", err)
	}
	serialized, err := media.SerializeLinkset(value, 1, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serialized, `title*=UTF-8''aAzZ09`) {
		t.Fatalf("international boundary characters = %q", serialized)
	}
	for _, relation := range []string{"a", "z", "A", "Z", "0", "9"} {
		candidate := mustMediaValue(t, `{"linkset":[{"`+relation+`":[{"href":"/"}]}]}`)
		if err := media.ValidateLinksetJSON(candidate, 1, 1_000); err != nil {
			t.Fatalf("relation %q error = %v", relation, err)
		}
	}
}

func TestLinksetDistinguishesMinimumLimitsAndLeadingControls(t *testing.T) {
	t.Parallel()

	value := mustMediaValue(t, `{"linkset":[{"next":[{"href":"/ok"}]}]}`)
	if err := media.ValidateLinksetJSON(value, 1, 1); !errors.Is(
		err, media.ErrLinksetLimit,
	) {
		t.Fatalf("minimum byte limit error = %v", err)
	}
	leadingControl := mustMediaValue(t, `{"linkset":[{"next":[{
		"href":"/ok","title*":[{"value":"\ninvalid"}]
	}]}]}`)
	if err := media.ValidateLinksetJSON(leadingControl, 1, 1_000); !errors.Is(
		err, media.ErrInvalidLinkset,
	) {
		t.Fatalf("leading control error = %v", err)
	}
}
