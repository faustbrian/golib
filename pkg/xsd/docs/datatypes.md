# Datatypes

The `datatype` package provides exact arbitrary-precision decimal and integer
values, built-in integer ranges, calendar and duration lexical checks, binary
lexical checks, and XML Name-derived lexical spaces. Decimal and integer
validation never converts through floating point or machine-width integers.

User-defined restriction, list, and union types are compiled into the schema
set. Supported length, enumeration, whitespace, and ordered facets are applied
during validation. The compiler rejects duplicate, malformed, inapplicable,
and base-invalid facets. Patterns on one restriction step are alternatives;
patterns inherited across restriction steps are cumulative. Calendar and
duration comparisons use exact arithmetic;
binary equality compares decoded octets; QName and NOTATION equality uses
expanded names with the value's namespace context. Patterns use XML Schema
whole-literal matching, Unicode category and block escapes, XML Name escapes,
and nested character-class subtraction while retaining linear-time matching.
NOTATION restrictions require enumerations that resolve to compiled notation
declarations; the built-in NOTATION type cannot be used directly.
The `TYPE-*` matrix rows identify the remaining datatype limitations.
