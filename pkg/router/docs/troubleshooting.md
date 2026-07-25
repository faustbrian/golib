# Troubleshooting

- `invalid route`: inspect the structured `Error.Field`; check method casing,
  absolute paths, wildcard identifiers, ASCII hosts, handlers, and limits.
- `route conflict`: compare method, canonical host wildcard shape, and path
  specificity. Registration order cannot resolve it.
- `duplicate route name`: rename or remove one published name; names are global
  across the compiled table.
- unexpected 405: inspect `Allow` and explicit methods. `GET` implies `HEAD`.
- unexpected 204: automatic OPTIONS is active; register OPTIONS or disable the
  policy explicitly.
- unexpected 414: raise `MaxRequestTargetBytes` only after measuring the
  legitimate path and query requirement.
- redirect instead of handler: inspect trailing slash and canonical path, or
  choose `RejectRedirects`.
- missing mount: `/prefix` redirects to `/prefix/` under the default policy;
  the mounted remainder route owns the latter boundary.
- URL generation error: supply exactly each required wildcard, use
  `Remainder` for `{name...}`, and reject dot segments before retrying.
