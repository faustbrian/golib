# Formatting

Core `Money.String` is a deterministic diagnostic representation such as
`12.30 EUR`. It is not locale display.

The optional `format` package accepts a validated `international/locale`
tag. It obtains CLDR digits, separators, sign, and currency symbol data from
`golang.org/x/text`, then applies them directly to the exact amount string. The
monetary amount is never converted to a float and is never rounded by the
formatter.

ISO codes are the default because they are unambiguous. Set `Options.Symbol`
for locale-specific symbols. Historic codes fall back to the ISO identity when
CLDR has no symbol.
