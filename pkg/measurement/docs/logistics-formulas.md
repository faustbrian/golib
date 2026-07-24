# Logistics Formulas

For a rectangular package with length `l`, width `w`, height `h`, quantity `q`,
truck interior width `tw`, and stacking factor `s`:

- floor area = `l * w`;
- package volume = `l * w * h`;
- total volume = `l * w * h * q`;
- loading metres = `(l * w / tw / s) * q`.

Length inputs are converted to canonical metres before derived arithmetic.
`TruckWidth` and `StackingFactor` must be positive. Package quantity is in
`[1, MaxPackageQuantity]`. A stacking factor of one means no stacking; two
means two equivalent packages share one floor position.

A volumetric divisor `d` with explicit volume unit computes
`volume-in-that-unit / d` kilograms. For example, `24000 cm3 / 5000 cm3/kg`
is `4.8 kg`. A volumetric index is density and computes `density * volume`.
These are mathematical inputs, not embedded carrier tariff rules; callers own
the divisor or index and its effective date.

## Limitations

The package does not decide package orientation, whether cargo is stackable,
or whether multiple packages can be placed side by side. It does not round
dimensions up to a carrier increment, select the greater of actual and
dimensional weight, or round the final chargeable weight. Apply those
carrier- and contract-specific rules before or after these raw formulas with
an explicit rounding policy.
