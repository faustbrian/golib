# Threat model

## Protected

- plaintext fixed-size records on temporary storage;
- undetected modification, truncation, duplication, position reordering,
  cross-linking, and cross-store or cross-chunk substitution;
- accidental group or world access to newly created work directories and
  chunks;
- redirection of work-directory, chunk, and cleanup operations through a
  replaced parent pathname ancestor;
- unbounded caller-controlled records, chunk memory, or merge file fan-in; and
- sensitive values appearing in public errors.

## Caller responsibilities

- derive and isolate a 32-byte key for each sensitive dataset;
- protect process memory and the parent directory;
- use an encrypted ephemeral filesystem;
- call `Close` and monitor cleanup failures;
- apply descriptor-relative crash-recovery cleanup without following links or
  deleting live work; and
- select bounds appropriate for the source population.

## Outside scope

Privileged host compromise, memory inspection, malicious bind mounts or device
files introduced inside the trusted root, whole-filesystem rollback, entropy
subsystem compromise, traffic analysis from file sizes, and availability
attacks within approved bounds are outside scope.
