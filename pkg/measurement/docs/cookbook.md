# Cookbook

## Carrier dimensions

Construct each side in the payload's declared unit and pass the package count
to `NewDimensions`. Call `CubicVolume` with the carrier's expected output unit.

## Loading metres

Construct a positive `TruckWidth` from configuration and a positive
`StackingFactor` from shipment facts. Do not embed a carrier default in shared
code. Call `LoadingMetres` with an exact context when all inputs terminate.

## Volumetric weight

Represent `5000 cm3/kg` as
`NewVolumetricDivisor(decimal.New(5000), CubicCentimetre)`. Represent an index
such as `200 kg/m3` with `NewVolumetricIndex`. Keep carrier selection and tariff
dates outside this package.

## Display

Use `FormatOptions` to name target unit, conversion rounding, final display
scale, display rounding, and separator. Do not parse localized display text
with `SymbolProfile`; supply an explicit locale-owned alias profile.
