# Holiday datasets

No holiday dataset is bundled. Applications supply calendars because one
country does not have one universal business policy.

Any future optional dataset must ship separately and include authoritative
source URL, license, provider, effective version, checksum, generation tool
version, deterministic normalized output, compatibility diff classification,
and a maintenance owner. `make provenance` fails if files appear under
`datasets/` without replacing the no-dataset gate with a dedicated verifier.
