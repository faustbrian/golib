# Tax and discounts

Tax rates are exact nonnegative fractions bounded at 1000%. Discount rates are
exact fractions in the inclusive range from 0% through 100%.

`AddTax` multiplies net by the rate, rounds the tax once, and derives gross by
addition. Therefore `net + tax == gross`.

`ExtractTax` divides gross by `1 + rate`, rounds net once, and derives tax by
subtraction. Therefore `net + tax == gross`, even when extraction cannot invert
an earlier exclusive calculation after rounding.

`ApplyDiscount` rounds the discount component once and derives final by
subtraction. Therefore `final + discount == original`.

Operation order is part of the contract. Applications should persist the
selected rounding mode and whether tax was calculated from net or extracted
from gross when audit reproduction requires it.
