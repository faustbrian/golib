// Package tomlwire provides bounded, strict TOML document decoding and
// deterministic encoding.
//
// TOML duplicate keys and malformed trailing text are always rejected.
// Datetime values use time.Time and TOML local date/time types according to
// the underlying decoder. Numeric conversions reject overflow and precision
// loss. TOML is not included in wire.DetectFormat because arbitrary text
// cannot be identified reliably.
package tomlwire
