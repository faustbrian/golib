# Adoption guide

1. Inventory every length, mass, rotation rule, stock source, and packing
   objective. Do not infer units from bare numbers.
2. Choose a lattice that exactly represents accepted business quantities.
   Define any application rounding before calling this module.
3. Give every expanded item a stable instance ID and every box a stable type
   ID. Preserve those IDs through order and warehouse systems.
4. Start with `Heuristic.PackAll`, conservative limits, and independent
   verification. Record unpacked diagnostics rather than dropping items.
5. Add physical rules with fixtures for fragile, upright, support, load,
   linked, and incompatible cases.
6. Use the exact solver as a tiny-case oracle and compare solution quality on
   representative normalized orders.
7. Pin the module version, canonical fixtures, options, and seed before
   accepting output changes during upgrades.

Adoption is complete only when monitoring distinguishes invalid input, no
heuristic placement, resource exhaustion, and proven infeasibility. Do not
replace a production packer from runtime-only benchmarks; compare feasibility
and objective quality on identical semantics.
