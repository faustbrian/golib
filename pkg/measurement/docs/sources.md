# Standards And Formula Sources

The unit catalog was re-audited on 2026-07-20 against these primary sources:

- The [BIPM SI Brochure, ninth edition, 2026 update](https://www.bipm.org/en/publications/si-brochure/)
  defines metre, kilogram, kelvin, coherent derived dimensions, decimal SI
  prefixes, the unit one, and the accepted litre relation. Prefix powers are
  applied to the whole powered unit, so `cm2 = 10^-4 m2` and
  `cm3 = 10^-6 m3`.
- [NIST SP 811 Appendix B](https://www.nist.gov/pml/special-publication-811/nist-guide-si-appendix-b-conversion-factors)
  distinguishes exact factors. The catalog uses the exact international inch
  `0.0254 m` and international pound `0.45359237 kg`; foot is 12 inches, yard
  is 3 feet, and avoirdupois ounce is 1/16 pound. Area and volume factors are
  exact powers of those length definitions.
- [NIST exact temperature conversions](https://www.nist.gov/pml/owm/si-units-temperature)
  define `C = K - 273.15` and `C = (F - 32) / 1.8`. The implementation uses
  the algebraically identical exact affine form `K = (F + 459.67) * 5 / 9`.
- [UN/CEFACT code list 6311](https://service.unece.org/trade/uncefact/vocabulary/uncl6311/)
  identifies loading metres as vehicle capacity requiring the complete width
  and height over a length. It supports the separate semantic dimension; it
  does not prescribe a universal trailer width or stacking rule.

Density factors are algebraic consequences of the pinned mass and volume
units: `1 g/cm3 = 1000 kg/m3`. Loading-metre truck width, stacking factor,
volumetric divisor, and volumetric index are explicit caller inputs. They are
not presented as SI constants or universal carrier rules.

Every factor has a canonical conversion fixture in `unit_definitions_test.go`.
Changes to a source version or interpretation require updating this register,
the fixture, round-trip properties, and the changelog in one review.

The ASCII wire symbols (`m2`, `m3`, `degC`, and `degF`) are stable API
identifiers, not typography claims. SI documents normally print superscripts
and `°C`; aliases and localized display forms remain caller-owned profile or
formatting policy.

Logistics fixtures are pinned to carrier material rather than treated as
universal physical constants:

- [DB Schenker international road terms effective 2026-05-04](https://www.dbschenker.com/resource/blob/2479672/3b7520da0b6a5db3138cbd14b70f3320/terms-of-provision-of-services-in-international-road-forwarding-from-4-05-2026-data.pdf)
  define `L x W / 2.4` loading metres and carrier-specific pre-rounding rules.
- [DHL's volumetric-weight reference](https://dct.dhl.com/help) defines the
  standard `cm3 / 5000` kilogram fixture.
- [FedEx dimensional-weight guidance](https://www.fedex.com/en-gb/how-to/calculate-costs/dimensional-weight.html)
  supplies the `36 x 25 x 16 cm` carrier example and tariff rounding context.
- [DSV's road-freight example](https://www.dsv.com/it-it/sostegno/faq/calcolatore-di-peso-volumetrico)
  supplies the `1.2 x 0.8 x 1 m` Euro-pallet fixture at `333 kg/m3`.

`logistics_fixtures_test.go` verifies these raw formulas. Carrier rules that
round dimensions or final chargeable weight are deliberately outside the raw
formula types and must be applied explicitly by the caller.
