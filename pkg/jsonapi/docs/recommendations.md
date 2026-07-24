# JSON:API recommendations

The [official recommendations](https://jsonapi.org/recommendations/) are
useful consistency guidance but are not normative JSON:API requirements. This
matrix deliberately keeps them separate from conformance claims.

| Recommendation and primary section | Package position | Responsibility |
| --- | --- | --- |
| [Naming](https://jsonapi.org/recommendations/#naming) | Valid core names accepted; recommended style not enforced | application schema |
| [Collection and resource URL design](https://jsonapi.org/recommendations/#url-design) | Adopt in examples; not enforced | application router |
| [Relationship and related-resource URLs](https://jsonapi.org/recommendations/#relationship-urls-and-related-resource-urls) | Supported by model; not auto-generated | application router |
| [Recommended response links](https://jsonapi.org/recommendations/#including-top-level-resource-level-and-relationship-links) | Supported by model | application response builder |
| [Filtering](https://jsonapi.org/recommendations/#filtering) | `filter[...]` preserved as an explicit hook | application defines operators |
| [Date and time fields](https://jsonapi.org/recommendations/#formatting-date-and-time-fields) | No domain value format imposed | application schema |
| [Asynchronous processing](https://jsonapi.org/recommendations/#asynchronous-processing) | No queue assumptions | application HTTP workflow |
| [Clients lacking PATCH](https://jsonapi.org/recommendations/#supporting-clients-lacking-patch) | Not implemented in core | optional middleware |
| [Authoring profiles](https://jsonapi.org/recommendations/#authoring-profiles) | Registration and document validation hooks | profile author and application |

## Adopted conventions in project examples

Examples use plural collection URLs, stable resource type names, relationship
links, bracketed filter families, comma-separated include/field/sort values,
and explicit pagination links. These choices improve consistency but the core
does not reject alternative valid URL designs or application semantics.

## Intentionally not enforced

- URL path naming and pluralization
- router method selection
- filter operator vocabulary
- default include or sparse-fieldset policy
- asynchronous job resources
- method-override headers
- domain error titles and localization

Enforcing these in a transport-neutral document package would blur
recommendations into normative rules and couple unrelated applications.
