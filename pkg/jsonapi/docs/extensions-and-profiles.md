# Extensions and profiles

JSON:API extensions and profiles have different authority. Extensions may add
namespaced specification semantics; profiles may define implementation
semantics without changing base JSON:API rules. The package keeps these seams
separate.

## Official support matrix

| URI | Kind | Status | API |
| --- | --- | --- | --- |
| `https://jsonapi.org/ext/atomic` | Extension | Complete | Atomic document, codec, validation, execution |
| `http://jsonapi.org/profiles/ethanresnick/cursor-pagination/` | Profile | Complete | Cursor query, links, metadata, and errors |

The Cursor profile URI intentionally uses `http`, matching the normative
profile document. Error type links use the HTTPS URIs defined by that profile.

## Register an extension

```go
codec, err := jsonapi.NewCodec(jsonapi.CodecOptions{
	Extensions: []jsonapi.ExtensionDefinition{{
		URI:       "https://example.com/extensions/version",
		Namespace: "version",
		Members: []jsonapi.MemberDefinition{{
			Scope: jsonapi.ResourceMemberScope,
			Name:  "version:id",
			Validate: func(value any) error {
				if _, ok := value.(string); !ok {
					return errors.New("version:id must be a string")
				}
				return nil
			},
		}},
	}},
})
```

Then decode or encode through `codec.Unmarshal` and `codec.Marshal`. Semantic
values appear in the relevant object's `AdditionalMembers` map.

Supported scopes:

- `TopLevelMemberScope`
- `ResourceMemberScope`
- `RelationshipMemberScope`
- `IdentifierMemberScope`
- `JSONAPIMemberScope`
- `ErrorMemberScope`
- `ErrorSourceMemberScope`
- `LinksObjectMemberScope`
- `LinkObjectMemberScope`

`LinksObjectMemberScope` and `LinkObjectMemberScope` are distinct. The first
registers a namespaced entry directly inside a `links` object, whose value may
have any extension-defined JSON shape. The second registers a member inside
an individual link object. Use `ExtensionLinkValue` and `Link.ExtensionValue`
to construct and inspect the former; individual link-object members remain in
`LinkObject.AdditionalMembers`.

An extension definition must have an absolute unique URI, a valid unique
namespace, and names beginning with `namespace:`. The same member cannot be
registered twice in one scope.

## Register profile semantics

```go
codec, err := jsonapi.NewCodec(jsonapi.CodecOptions{
	Profiles: []jsonapi.ProfileDefinition{{
		URI: "https://example.com/profiles/timestamps",
		ValidateDocument: func(document jsonapi.Document) error {
			// Validate implementation semantics without weakening core rules.
			return validateTimestamps(document)
		},
	}},
})
```

Profile validators run after core validation. They return their own error so
applications can preserve a domain-specific error contract when desired.

## Media type registration

Codec registration controls document semantics. Negotiator registration
controls HTTP media type selection. Configure both from the same supported
URI set:

```go
negotiator, err := jsonapi.NewNegotiator(
	[]string{"https://example.com/extensions/version"},
	[]string{"https://example.com/profiles/timestamps"},
)
```

Unsupported request extensions produce a 415. Unknown request profiles remain
available because profiles cannot alter base semantics. During `Accept`
selection, candidates with unsupported extensions are ignored and unsupported
profiles are omitted from an otherwise acceptable representation.

## Extension design checklist

Before publishing an extension:

1. define a stable absolute URI and namespace;
2. specify each member's object scope and JSON value shape;
3. specify processing semantics and error behavior;
4. add registered-member round-trip and malformed-value tests;
5. add the URI to media negotiation configuration;
6. document whether query parameter families are introduced;
7. update conformance and compatibility matrices;
8. avoid encoding application behavior as an alleged core rule.

For an implementation-only convention that does not alter JSON:API document
semantics, prefer a query-family hook or application adapter instead of an
extension.
