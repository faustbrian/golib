// Package money provides immutable, bounded, exact monetary values backed by
// math and identified by international currency codes.
//
// Fixed Money never rounds implicitly. Multiplication and division produce
// RationalMoney, which requires an explicit context and rounding mode before it
// can return fixed Money. Currency and resolved context mismatches are errors.
package money
