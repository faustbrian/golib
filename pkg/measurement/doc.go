// Package measurement provides immutable exact logistics quantities with
// explicit units, dimensions, conversion policy, and arithmetic bounds.
//
// Decimal arithmetic is owned by github.com/faustbrian/golib/pkg/math/decimal. Core
// quantities never convert through float32 or float64. Conversions either
// require an exact terminating result or an explicit scale and rounding mode.
// Loading metres remain semantically distinct from ordinary lengths.
package measurement
