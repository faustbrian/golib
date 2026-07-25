package jsonschema

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"math/big"
	"strings"
)

func equalJSON(left *jsonValue, right *jsonValue) bool {
	if left.kind != right.kind {
		return false
	}

	switch left.kind {
	case kindNull:
		return true
	case kindBoolean:
		return left.boolean == right.boolean
	case kindNumber:
		return equalNumber(left.number, right.number)
	case kindString:
		return left.text == right.text
	case kindArray:
		if len(left.array) != len(right.array) {
			return false
		}
		for index := range left.array {
			if !equalJSON(left.array[index], right.array[index]) {
				return false
			}
		}

		return true
	case kindObject:
		if len(left.object) != len(right.object) {
			return false
		}
		for name, leftValue := range left.object {
			rightValue, exists := right.object[name]
			if !exists || !equalJSON(leftValue, rightValue) {
				return false
			}
		}

		return true
	default:
		return false
	}
}

func uniqueJSON(values []*jsonValue, state *evaluationState) (bool, error) {
	if len(values) <= 16 {
		return uniqueJSONPairwise(values, state)
	}
	return uniqueJSONWithHash(values, state, canonicalJSONHash)
}

func uniqueJSONPairwise(values []*jsonValue, state *evaluationState) (bool, error) {
	for left := range values {
		for right := left + 1; right < len(values); right++ {
			if err := state.consumeUniqueComparison(); err != nil {
				return false, err
			}
			if equalJSON(values[left], values[right]) {
				return false, nil
			}
		}
	}

	return true, nil
}

func uniqueJSONWithHash(
	values []*jsonValue,
	state *evaluationState,
	hashValue func(*jsonValue) [sha256.Size]byte,
) (bool, error) {
	seen := make(map[[sha256.Size]byte][]*jsonValue, len(values))
	for index, value := range values {
		if index > 0 {
			if err := state.consumeUniqueComparison(); err != nil {
				return false, err
			}
		}
		digest := hashValue(value)
		for collision, previous := range seen[digest] {
			if collision > 0 {
				if err := state.consumeUniqueComparison(); err != nil {
					return false, err
				}
			}
			if equalJSON(previous, value) {
				return false, nil
			}
		}
		seen[digest] = append(seen[digest], value)
	}

	return true, nil
}

func canonicalJSONHash(value *jsonValue) [sha256.Size]byte {
	digest := sha256.New()
	writeCanonicalJSONHash(digest, value)

	return [sha256.Size]byte(digest.Sum(nil))
}

func writeCanonicalJSONHash(digest hash.Hash, value *jsonValue) {
	_, _ = digest.Write([]byte{byte(value.kind)})
	switch value.kind {
	case kindNull:
	case kindBoolean:
		if value.boolean {
			_, _ = digest.Write([]byte{1})
		} else {
			_, _ = digest.Write([]byte{0})
		}
	case kindNumber:
		number := normalizeNumber(value.number)
		if number.negative {
			_, _ = digest.Write([]byte{1})
		} else {
			_, _ = digest.Write([]byte{0})
		}
		writeLengthPrefixedHash(digest, number.digits)
		writeLengthPrefixedHash(digest, number.scale.String())
	case kindString:
		writeLengthPrefixedHash(digest, value.text)
	case kindArray:
		writeHashLength(digest, len(value.array))
		for _, item := range value.array {
			writeCanonicalJSONHash(digest, item)
		}
	case kindObject:
		writeHashLength(digest, len(value.object))
		for _, name := range sortedStringKeys(value.object) {
			writeLengthPrefixedHash(digest, name)
			writeCanonicalJSONHash(digest, value.object[name])
		}
	}
}

func writeLengthPrefixedHash(digest hash.Hash, value string) {
	writeHashLength(digest, len(value))
	_, _ = digest.Write([]byte(value))
}

func writeHashLength(digest hash.Hash, length int) {
	var encoded [binary.MaxVarintLen64]byte
	// #nosec G115 -- length always comes from len and is therefore nonnegative.
	size := binary.PutUvarint(encoded[:], uint64(length))
	_, _ = digest.Write(encoded[:size])
}

type normalizedNumber struct {
	negative bool
	digits   string
	scale    *big.Int
}

func equalNumber(left string, right string) bool {
	leftNumber := normalizeNumber(left)
	rightNumber := normalizeNumber(right)

	return leftNumber.negative == rightNumber.negative &&
		leftNumber.digits == rightNumber.digits &&
		leftNumber.scale.Cmp(rightNumber.scale) == 0
}

func normalizeNumber(number string) normalizedNumber {
	negative := strings.HasPrefix(number, "-")
	unsigned := strings.TrimPrefix(number, "-")
	mantissa, exponentText, hasExponent := strings.Cut(unsigned, "e")
	if !hasExponent {
		mantissa, exponentText, hasExponent = strings.Cut(unsigned, "E")
	}

	exponent := new(big.Int)
	if hasExponent {
		exponent.SetString(strings.TrimPrefix(exponentText, "+"), 10)
	}

	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return normalizedNumber{digits: "0", scale: new(big.Int)}
	}

	trailingZeros := len(digits) - len(strings.TrimRight(digits, "0"))
	digits = strings.TrimRight(digits, "0")
	scale := new(big.Int).Sub(exponent, big.NewInt(int64(len(fraction))))
	scale.Add(scale, big.NewInt(int64(trailingZeros)))

	return normalizedNumber{negative: negative, digits: digits, scale: scale}
}

func compareNumber(left string, right string) int {
	leftNumber := normalizeNumber(left)
	rightNumber := normalizeNumber(right)

	if leftNumber.digits == "0" && rightNumber.digits == "0" {
		return 0
	}
	if leftNumber.digits == "0" {
		if rightNumber.negative {
			return 1
		}

		return -1
	}
	if rightNumber.digits == "0" {
		if leftNumber.negative {
			return -1
		}

		return 1
	}
	if leftNumber.negative != rightNumber.negative {
		if leftNumber.negative {
			return -1
		}

		return 1
	}

	comparison := compareNumberMagnitude(leftNumber, rightNumber)
	if leftNumber.negative {
		return -comparison
	}

	return comparison
}

func compareNumberMagnitude(left normalizedNumber, right normalizedNumber) int {
	leftOrder := new(big.Int).Add(left.scale, big.NewInt(int64(len(left.digits))))
	rightOrder := new(big.Int).Add(right.scale, big.NewInt(int64(len(right.digits))))
	if comparison := leftOrder.Cmp(rightOrder); comparison != 0 {
		return comparison
	}

	length := max(len(left.digits), len(right.digits))
	for index := range length {
		leftDigit := byte('0')
		if index < len(left.digits) {
			leftDigit = left.digits[index]
		}
		rightDigit := byte('0')
		if index < len(right.digits) {
			rightDigit = right.digits[index]
		}
		if leftDigit < rightDigit {
			return -1
		}
		if leftDigit > rightDigit {
			return 1
		}
	}

	return 0
}

func numberIsMultiple(number string, divisor string) bool {
	if divisor == "" {
		return false
	}
	value := normalizeNumber(number)
	if value.digits == "0" {
		return true
	}
	factor := normalizeNumber(divisor)
	delta := new(big.Int).Sub(value.scale, factor.scale)
	if delta.Sign() < 0 {
		return false
	}

	valueInteger := new(big.Int)
	valueInteger.SetString(value.digits, 10)
	factorInteger := new(big.Int)
	factorInteger.SetString(factor.digits, 10)
	gcd := new(big.Int)
	gcd.GCD(nil, nil, valueInteger, factorInteger)
	remaining := new(big.Int).Quo(factorInteger, gcd)
	if remaining.Cmp(big.NewInt(1)) == 0 {
		return true
	}

	twos := removeFactor(remaining, 2)
	fives := removeFactor(remaining, 5)
	if remaining.Cmp(big.NewInt(1)) != 0 {
		return false
	}

	return delta.Cmp(big.NewInt(int64(twos))) >= 0 &&
		delta.Cmp(big.NewInt(int64(fives))) >= 0
}

func removeFactor(value *big.Int, factor int64) int {
	count := 0
	divisor := big.NewInt(factor)
	remainder := new(big.Int)
	for range value.BitLen() {
		quotient, modulus := new(big.Int).QuoRem(value, divisor, remainder)
		if modulus.Sign() != 0 {
			return count
		}
		value.Set(quotient)
		count++
	}

	return count
}
