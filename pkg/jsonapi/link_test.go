package jsonapi

import (
	"errors"
	"testing"
)

func TestLinkObjectSupportsEveryJSONAPI11Member(t *testing.T) {
	t.Parallel()

	describedBy := URI("https://example.com/schemas/comments")
	document := Document{
		Data: NullData(),
		Links: Links{
			"related": LinkFromObject(LinkObject{
				Href:        "https://example.com/articles/1/comments",
				Rel:         "related",
				DescribedBy: &describedBy,
				Title:       "Comments",
				Type:        "application/vnd.api+json",
				Hreflang:    LanguageTags("en", "fi"),
				Meta:        Meta{"count": 10},
			}),
		},
	}

	got, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	want := `{"links":{"related":{"href":"https://example.com/articles/1/comments","rel":"related","describedby":"https://example.com/schemas/comments","title":"Comments","type":"application/vnd.api+json","hreflang":["en","fi"],"meta":{"count":10}}},"data":null}`
	if string(got) != want {
		t.Fatalf("unexpected JSON:\n got: %s\nwant: %s", got, want)
	}
}

func TestLinkObjectRoundTripPreservesHreflangShape(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"scalar": `{"links":{"self":{"href":"/articles","hreflang":"en"}},"data":null}`,
		"array":  `{"links":{"self":{"href":"/articles","hreflang":["en","fi"]}},"data":null}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document, err := Unmarshal([]byte(payload))
			if err != nil {
				t.Fatalf("unmarshal document: %v", err)
			}
			got, err := Marshal(document)
			if err != nil {
				t.Fatalf("marshal document: %v", err)
			}
			if string(got) != payload {
				t.Fatalf("unexpected round trip: got %s, want %s", got, payload)
			}
		})
	}
}

func TestLinkObjectRoundTripSupportsNestedDescribedByObject(t *testing.T) {
	t.Parallel()

	payload := `{"links":{"self":{"href":"/articles","describedby":{"href":"/schema","type":"application/schema+json"}}},"data":null}`
	document, err := Unmarshal([]byte(payload))
	if err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	got, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("unexpected round trip: got %s, want %s", got, payload)
	}
}

func TestValidateRejectsInvalidLinkObjectMembers(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		object LinkObject
		path   string
		code   string
	}{
		"missing href": {
			object: LinkObject{Href: "/unused"},
			path:   "/links/self/href",
			code:   "required",
		},
		"invalid relation": {
			object: LinkObject{Href: "/articles", Rel: "bad relation"},
			path:   "/links/self/rel",
			code:   "link-relation",
		},
		"invalid describedby": {
			object: LinkObject{Href: "/articles", DescribedBy: linkPointer(URI(":bad"))},
			path:   "/links/self/describedby",
			code:   "uri-reference",
		},
		"invalid media type": {
			object: LinkObject{Href: "/articles", Type: "not a media type"},
			path:   "/links/self/type",
			code:   "media-type",
		},
		"invalid language tag": {
			object: LinkObject{Href: "/articles", Hreflang: LanguageTag("not_a_tag")},
			path:   "/links/self/hreflang",
			code:   "language-tag",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			link := LinkFromObject(test.object)
			if name == "missing href" {
				link = Link{object: true}
			}
			err := (Document{
				Data:  NullData(),
				Links: Links{"self": link},
			}).Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if !hasViolation(validationError, test.path, test.code) {
				t.Fatalf(
					"missing violation path %q code %q in %#v",
					test.path,
					test.code,
					validationError.Violations,
				)
			}
		})
	}
}

func TestLinkURIReferencesRequireRFC3986WireEncoding(t *testing.T) {
	t.Parallel()

	for _, href := range []string{
		"/bad path", "/snow/雪", "/items/[draft]", "?q=a b", "#a b", "//host:abc",
		"//user@@host", "a^:value",
	} {
		err := (Document{Data: NullData(), Links: Links{"self": URI(href)}}).Validate()
		var validationError *ValidationError
		if !errors.As(err, &validationError) ||
			!hasViolation(validationError, "/links/self", "uri-reference") {
			t.Fatalf("non-RFC URI-reference %q was accepted: %v", href, err)
		}
	}
	for _, href := range []string{
		"/good%20path", "/snow/%E9%9B%AA", "/items/%5Bdraft%5D", "?q=a%20b", "#",
		"https://[v1.foo]/",
	} {
		if err := (Document{Data: NullData(), Links: Links{"self": URI(href)}}).Validate(); err != nil {
			t.Fatalf("valid URI-reference %q was rejected: %v", href, err)
		}
	}
}

func TestRegisteredLinkRelationRejectsUnderscore(t *testing.T) {
	t.Parallel()

	link := LinkFromObject(LinkObject{Href: "/articles", Rel: "version_history"})
	err := (Document{Data: NullData(), Links: Links{"self": link}}).Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/links/self/rel", "link-relation") {
		t.Fatalf("invalid registered relation was accepted: %T %#v", err, validationError)
	}
}

func TestConstructedDescribedByChainUsesDecodeDepthLimit(t *testing.T) {
	t.Parallel()

	link := URI("/")
	for range DefaultMaxNestingDepth + 1 {
		describedBy := link
		link = LinkFromObject(LinkObject{Href: "/", DescribedBy: &describedBy})
	}
	err := (Document{Data: NullData(), Links: Links{"self": link}}).Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("over-deep constructed link chain was accepted: %T %#v", err, validationError)
	}
	for _, violation := range validationError.Violations {
		if violation.Code == "limit" {
			return
		}
	}
	t.Fatalf("over-deep constructed link chain lacks limit error: %#v", validationError)
}

func TestConstructedDescribedByCycleIsRejected(t *testing.T) {
	t.Parallel()

	var link Link
	link = LinkFromObject(LinkObject{Href: "/", DescribedBy: &link})
	err := (Document{Data: NullData(), Links: Links{"self": link}}).Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/links/self/describedby/describedby", "cycle") {
		t.Fatalf("constructed link cycle was accepted: %T %#v", err, validationError)
	}
}

func TestConfiguredCodecRejectsConstructedDescribedByCycle(t *testing.T) {
	t.Parallel()

	var link Link
	link = LinkFromObject(LinkObject{Href: "/", DescribedBy: &link})
	codec := mustCodec(t, CodecOptions{})
	_, err := codec.Marshal(Document{Data: NullData(), Links: Links{"self": link}})
	var validationError *ValidationError
	if !errors.As(err, &validationError) ||
		!hasViolation(validationError, "/links/self/describedby/describedby", "cycle") {
		t.Fatalf("configured codec accepted link cycle: %T %#v", err, validationError)
	}
}

func TestConfiguredCodecBoundsConstructedDescribedByChain(t *testing.T) {
	t.Parallel()

	link := URI("/")
	for range DefaultMaxNestingDepth + 1 {
		describedBy := link
		link = LinkFromObject(LinkObject{Href: "/", DescribedBy: &describedBy})
	}
	codec := mustCodec(t, CodecOptions{})
	_, err := codec.Marshal(Document{Data: NullData(), Links: Links{"self": link}})
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("configured codec accepted deep link chain: %T %v", err, err)
	}
	for _, violation := range validationError.Violations {
		if violation.Code == "limit" {
			return
		}
	}
	t.Fatalf("configured deep chain lacks limit error: %#v", validationError)
}

func TestURIReferenceGrammarHelpers(t *testing.T) {
	t.Parallel()

	for _, authority := range []string{
		"", "example.com", "example.com:", "user:pass@example.com:443", "[::1]:8080",
	} {
		if !validURIAuthority(authority) {
			t.Fatalf("valid authority rejected: %q", authority)
		}
	}
	for _, literal := range []string{"v12x", "vX.foo", "v1."} {
		if validIPLiteral(literal) {
			t.Fatalf("invalid IP literal accepted: %q", literal)
		}
	}
	for _, authority := range []string{
		"bad user@example.com", "user@[bad space]:80", "[::1]suffix", "bad[host", "host:abc",
		"bad host:80",
	} {
		if validURIAuthority(authority) {
			t.Fatalf("invalid authority accepted: %q", authority)
		}
	}
	for value, valid := range map[string]bool{"": true, ":": true, ":443": true, "443": false, ":x": false} {
		if validURIPort(value) != valid {
			t.Fatalf("unexpected port validity for %q", value)
		}
	}
	for _, value := range []string{"%", "%x0", "%0x"} {
		if validURIComponent(value, "") {
			t.Fatalf("invalid encoded component accepted: %q", value)
		}
	}
	for _, value := range []string{
		"https://user:pass@example.com:443/path?query=value#fragment",
		"https://[2001:db8::1]/path",
		"mailto:user@example.com",
		"//example.com",
		"relative/path",
	} {
		if _, valid := parseURIReference(value); !valid {
			t.Fatalf("valid URI-reference rejected: %q", value)
		}
	}
}

func linkPointer(link Link) *Link {
	return &link
}
