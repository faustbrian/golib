package money

const invariantViolation = "money: internal invariant violated"

func mustInvariant[T any](value T, err error) T {
	if err != nil {
		panic(invariantViolation)
	}

	return value
}
