# Threat model

## Protected

- plaintext fixed-size records on temporary storage;
- undetected modification, truncation, position reordering, and cross-chunk
  substitution;
- accidental group or world access to newly created work directories and
  chunks;
- unbounded caller-controlled records, chunk memory, or merge file fan-in; and
- sensitive values appearing in public errors.

## Caller responsibilities

- derive and isolate a 32-byte key for each sensitive dataset;
- protect process memory and the parent directory;
- use an encrypted ephemeral filesystem;
- call `Close` and monitor cleanup failures;
- apply crash-recovery cleanup without deleting live work; and
- select bounds appropriate for the source population.

## Outside scope

Privileged host compromise, memory inspection, malicious replacement of parent
path ancestors, whole-filesystem rollback, entropy subsystem compromise,
traffic analysis from file sizes, and availability attacks within approved
bounds are outside scope.
