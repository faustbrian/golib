# Model

A `Path` is an ordered sequence of validated segments. Dots, slashes,
backslashes, NUL, control characters, empty segments, and traversal segments
are rejected so the dotted display form cannot collide.

A `Value` has exactly one `Kind`: missing, null, bool, int64, finite float64,
string, time, duration, or list. Lists recursively copy their input and output.
A `Fact` binds a path to a value and records whether the subject, resource, or
environment supplied it. Ownership is provenance only.

`Context` copies every fact and rejects duplicate paths. A lookup absent from
the snapshot returns `Missing`; a supplied `Null` remains null.

Rules carry a stable ID, namespace, priority, tags, proposition, and optional
literal derived facts. A rule set adds its own ID, namespace, and conflict
strategy. None of these fields imply authorization, rollout, validation, or
action semantics.
