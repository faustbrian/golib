# API integration examples

Localized fields remain ordinary JSON objects. Protocol adapters must not add
matching, fallback, pluralization, or a default locale.

## JSON:API

Place the canonical object directly in an attribute accepted by jsonapi:

```go
encoded, err := localized.EncodeJSON(title)
if err != nil {
    return err
}
var attribute map[string]string
if err := json.Unmarshal(encoded, &attribute); err != nil {
    return err
}
resource.Attributes["title"] = attribute
```

The resulting document shape is:

```json
{
  "data": {
    "type": "articles",
    "id": "42",
    "attributes": {
      "title": {"en-US": "Hello", "fi": "Hei"}
    }
  }
}
```

Decode the attribute through `localized.DecodeJSON` at the domain boundary.
JSON:API sparse fields select the whole `title` attribute, not one locale.

## JSON-RPC

Use the same object in jsonrpc params or results:

```json
{
  "jsonrpc": "2.0",
  "id": "request-1",
  "method": "article.rename",
  "params": {
    "article_id": "42",
    "title": {"en-US": "Hello", "fi": ""}
  }
}
```

The empty Finnish value is present; omission of `fi` is missing. Invalid
localized input is a method-parameter error selected by the application, not a
new JSON-RPC error category.

## OpenRPC

Describe a localized field as an object with string values and document the
canonical BCP 47 key contract:

```json
{
  "title": "LocalizedText",
  "type": "object",
  "additionalProperties": {"type": "string"},
  "description": "Canonical BCP 47 keys; missing and present-empty differ; no implicit fallback",
  "examples": [
    {"en-US": "Hello", "fi": "Hei"}
  ]
}
```

OpenRPC/JSON Schema cannot express the complete pinned registry membership
contract as a key regex. Parse keys with `international/locale` through this
package rather than claiming regex equivalence.
