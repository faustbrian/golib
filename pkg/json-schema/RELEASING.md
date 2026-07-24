# Releasing

Do not create `json-schema/v1.0.0` until all of these are true:

- every mandatory and optional released-dialect official case passes with
  zero failures and zero unexplained skips;
- every Core and Validation requirement maps to implementation, tests, and
  documentation;
- official meta-validation, references, dynamic scope, vocabularies,
  annotations, formats, content, and all output forms have no known gap;
- Bowtie runs all six dialects reproducibly and published reports are current;
- meaningful production statement coverage is 100%;
- race, fuzz, mutation, vulnerability, API, documentation, benchmark,
  dependency, license, secret, workflow-security, and owned-analysis gates
  pass;
- security limits and threat behavior are tested and documented;
- every public API has documentation and executable examples;
- changelog, compatibility notes, and benchmark evidence are current.

For a release candidate, run every local gate from a clean checkout, compare
the generated conformance manifest, review dependencies and licenses, build
the Bowtie image, inspect the full diff from the prior tag, and obtain review.
Tag from the verified commit with the directory-prefixed semantic version.
Release notes must distinguish normative behavior, implementation policy,
optional capabilities, convenience APIs, and any remaining non-v1 limitation.
