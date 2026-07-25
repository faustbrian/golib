# Dialect and keyword matrices

Legend: ✓ is defined by the dialect; form notes identify incompatible shapes.
Unknown keywords do not assert and may contribute annotations where the
dialect's annotation model applies.

## Core behavior

| Behavior | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| Identifier | `id` | `id` | `$id` | `$id` | `$id` | `$id` |
| Boolean schemas | — | — | ✓ | ✓ | ✓ | ✓ |
| `$schema` dialect declaration | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `$ref` | replaces siblings | replaces siblings | replaces siblings | replaces siblings | applicator | applicator |
| JSON Pointer fragment | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Plain-name anchor | `id` fragment | `id` fragment | `$id` fragment | `$id` fragment | `$anchor` | `$anchor` |
| Definitions container | `definitions` | `definitions` | `definitions` | `definitions` | `$defs` | `$defs` |
| Vocabulary declaration | — | — | — | — | `$vocabulary` | `$vocabulary` |
| Recursive reference | — | — | — | — | `$recursiveRef` | — |
| Dynamic reference | — | — | — | — | — | `$dynamicRef` |
| Embedded/compound resources | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Missing resource | typed error | typed error | typed error | typed error | typed error | typed error |
| Meta-schema validation | official | official | official | official | official + vocabularies | official + vocabularies |

Anchor indexing is dialect-isolated: `$anchor` is recognized only in 2019-09
and 2020-12, `$recursiveAnchor` only in 2019-09, and `$dynamicAnchor` only in
2020-12. In every earlier dialect those spellings remain unknown keywords and
cannot create reference targets.

Active `id` and `$id` values must be valid URI references. Duplicate resolved
resource identifiers and duplicate plain-name anchors within one resource are
rejected as `ErrInvalidSchema`; an anchor cannot silently replace another
`$anchor`, `$dynamicAnchor`, or legacy identifier fragment.
Equivalent RFC 3986 spellings share one resource identity after case, default
port, dot-segment, and unreserved-percent normalization.

## Validation keywords

| Keyword | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| `type` | name/array/schema union | name/array | ✓ | ✓ | ✓ | ✓ |
| `disallow` | ✓ | — | — | — | — | — |
| `enum` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `const` | — | — | ✓ | ✓ | ✓ | ✓ |
| `divisibleBy` | ✓ | — | — | — | — | — |
| `multipleOf` | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `minimum`, `maximum` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `exclusiveMinimum`, `exclusiveMaximum` | boolean modifier | boolean modifier | numeric | numeric | numeric | numeric |
| `minLength`, `maxLength`, `pattern` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `format` | explicit assertion policy | explicit assertion policy | explicit assertion policy | explicit assertion policy | annotation/assertion vocabularies | annotation/assertion vocabularies |
| `minItems`, `maxItems`, `uniqueItems` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `contains` | — | — | ✓ | ✓ | ✓ | ✓ |
| `minContains`, `maxContains` | — | — | — | — | ✓ | ✓ |
| `minProperties`, `maxProperties` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `required` | property boolean | string array | string array | string array | string array | string array |
| `dependentRequired` | — | — | — | — | ✓ | ✓ |

Numbers retain exact decimal and exponent semantics. Draft 3/4 integer checks
use their lexical-era rules; Draft 6 and later use mathematical integers.
String lengths count Unicode code points. Equality and uniqueness use JSON
structural equality with exact numeric equality.

## Applicator keywords

| Keyword | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| `properties`, `patternProperties` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `additionalProperties` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `propertyNames` | — | — | ✓ | ✓ | ✓ | ✓ |
| `dependencies` | string/array/schema | array/schema | array/schema | array/schema | compatibility | compatibility |
| `dependentSchemas` | — | — | — | — | ✓ | ✓ |
| Tuple `items` array | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| `additionalItems` | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| `prefixItems` | — | — | — | — | — | ✓ |
| Single-schema `items` | ✓ | ✓ | ✓ | ✓ | ✓ | post-prefix |
| `allOf`, `anyOf`, `oneOf`, `not` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `extends` | ✓ | — | — | — | — | — |
| `if`, `then`, `else` | — | — | — | ✓ | ✓ | ✓ |
| `unevaluatedItems`, `unevaluatedProperties` | — | — | — | — | ✓ | ✓ |

Applicator annotations propagate only from successful paths. Evaluated item
and property sets merge through references, valid combinator branches,
conditionals, dependent schemas, contains, and nested unevaluated applicators.

## Content and metadata

| Keyword | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| `title`, `description`, `default` | annotation | annotation | annotation | annotation | annotation | annotation |
| `examples` | — | — | annotation | annotation | annotation | annotation |
| `readOnly`, `writeOnly` | — | — | — | annotation | annotation | annotation |
| `deprecated` | — | — | — | — | annotation | annotation |
| `contentEncoding`, `contentMediaType` | — | — | — | annotation / opt-in assertion | annotation / opt-in assertion | annotation / opt-in assertion |
| `contentSchema` | — | — | — | — | annotation | annotation |

Content assertion recognizes RFC 4648 base64 and JSON or `+json` media types.
Unknown encodings and media types remain annotations. `contentSchema`
contributes an annotation only for strings when `contentMediaType` is present.

## Standard formats

| Format | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| `color` | ✓ | — | — | — | — | — |
| `date` | ✓ | — | — | ✓ | ✓ | ✓ |
| `date-time`, `email`, `ipv6`, `uri` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `duration`, `uuid` | — | — | — | — | ✓ | ✓ |
| `host-name`, `ip-address` | ✓ | — | — | — | — | — |
| `hostname`, `ipv4` | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `idn-email`, `idn-hostname`, `iri`, `iri-reference` | — | — | — | ✓ | ✓ | ✓ |
| `json-pointer`, `uri-reference`, `uri-template` | — | — | ✓ | ✓ | ✓ | ✓ |
| `regex`, `time` | ✓ | — | — | ✓ | ✓ | ✓ |
| `relative-json-pointer` | — | — | — | ✓ | ✓ | ✓ |

Built-in formats activate only in the dialects shown. A caller can explicitly
register any application-defined format name, including replacing a built-in;
that instance-owned registration is an application policy and can extend any
dialect. Unknown unregistered format names remain annotations.

## Output and annotation behavior

Flag is available for every dialect. The standard Basic, Detailed, and
Verbose location model is exposed for all compiled schemas and follows the
2019-09/2020-12 output schema where those dialects define it. Keyword and
instance locations are JSON Pointers; absolute locations use canonical schema
resource identifiers. Annotation behavior is executed against every
official annotation case compatible with each dialect.
