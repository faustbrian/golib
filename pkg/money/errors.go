package money

import "errors"

var (
	// ErrUnknownCurrency reports an absent or unrecognized currency identity.
	ErrUnknownCurrency = errors.New("money: unknown currency")
	// ErrMinorUnitsUnavailable reports that ISO metadata defines no minor-unit
	// exponent for a currency, so a default context cannot be inferred.
	ErrMinorUnitsUnavailable = errors.New("money: currency has no minor-unit metadata")
	// ErrInvalidContext reports an impossible or unsupported monetary context.
	ErrInvalidContext = errors.New("money: invalid context")
	// ErrPrecisionLoss reports input that cannot fit its context without
	// discarding represented decimal places.
	ErrPrecisionLoss = errors.New("money: precision loss")
	// ErrCurrencyMismatch reports arithmetic across different currencies.
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	// ErrContextMismatch reports arithmetic across different precision policies.
	ErrContextMismatch = errors.New("money: context mismatch")
	// ErrInvalidMoney reports use of an absent Money zero value.
	ErrInvalidMoney = errors.New("money: invalid value")
	// ErrAmountLimit reports an amount outside configured digit or scale bounds.
	ErrAmountLimit = errors.New("money: amount limit exceeded")
	// ErrInvalidRate reports a malformed, negative, or excessive exact rate.
	ErrInvalidRate = errors.New("money: invalid rate")
	// ErrInvalidAllocation reports an empty, excessive, or nonpositive split.
	ErrInvalidAllocation = errors.New("money: invalid allocation")
	// ErrMoneyBagLimit reports excessive distinct currency/context entries.
	ErrMoneyBagLimit = errors.New("money: bag limit exceeded")
)
